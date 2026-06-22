-- Módulo público de la extensión `agent` (S39): el motor headless.
--
-- Implementa el contrato de [agente.md](../../../../../docs/agente.md):
--
--   §2 El TURNO (`Session:send`): anexa el mensaje del usuario, ensambla el
--      request canónico (§7), lo pasa por hooks `request.pre`, llama al adaptador
--      (`stream`), consume el stream canónico de Events (providers.md §2.3:
--      text/thinking/tool_call.*/usage/done) re-emitiéndolos en el bus
--      (`agent:delta`), persiste el mensaje del `done`, y si `stop_reason ==
--      "tool_calls"` ejecuta cada tool en orden (permisos → tool.pre → handler →
--      tool.post → tool_result) y VUELVE a pedir; termina cuando el modelo para
--      sin tools o se agota `max_turns`.
--   §3 Registro de TOOLS (`agent.tool`): nombre, descripción, schema, handler.
--   §4 HOOKS: notificaciones por el bus `nu.events` (`agent:*`, con atribución
--      obligatoria `session`, G3) y MIDDLEWARE por registro propio (`agent.hook`,
--      puntos `request.pre`/`tool.pre`/`tool.post`/`permission`/`compact`).
--   §5 PERMISOS: pipeline `deny` → `allow` → hooks `permission` → ask/headless;
--      en headless (sin `nu.ui`, `nu.has("ui")`=false, G20) y sin respuesta:
--      default DENY con error ACCIONABLE (nombra el patrón a añadir).
--   §10 Configuración (`agent.toml`): modelo por defecto, `max_turns`, permisos.
--
-- ADR-003: el core NO sabe lo que es un agente; todo es Lua puro sobre la API
-- pública (api.md) + las extensiones `providers`/`sessions`. Código de error de
-- la extensión: `EAGENT` (forma de los del core, api.md §1.4 / ADR-009).

local providers = require("providers")
local sessions = require("sessions")

local M = {}

-- max_turns por defecto (agente.md §2 paso 6 / §10): protección contra loops del
-- modelo. Una sesión puede subirlo/bajarlo por `opts.max_turns`.
local DEFAULT_MAX_TURNS = 32

-- ---------------------------------------------------------------------------
-- Errores estructurados de la extensión (EAGENT, agente.md / ADR-009).
-- ---------------------------------------------------------------------------

local function eagent(message, detail)
  error({ code = "EAGENT", message = message, detail = detail })
end

local function einval(message)
  error({ code = "EINVAL", message = message })
end

-- ---------------------------------------------------------------------------
-- Eventos `agent:*` por el bus del core (agente.md §4 notificaciones).
-- ---------------------------------------------------------------------------

-- Atribución obligatoria (G3, agente.md §4): TODO payload `agent:*` lleva
-- `session` (id de la sesión emisora). El campo se pone en un ÚNICO sitio (este
-- helper) para no olvidarlo nunca. El bus es el del core (`nu.events`), namespace
-- `agent:` (el del plugin, no reserva del core, ADR-003).
local function emit(session_id, name, payload)
  payload = payload or {}
  payload.session = session_id
  nu.events.emit("agent:" .. name, payload)
end

-- ---------------------------------------------------------------------------
-- Registro de TOOLS (agente.md §3).
-- ---------------------------------------------------------------------------

-- Registro vivo (en memoria) de tools por nombre. Hay UN ÚNICO registro para todo
-- el proceso (agente.md §9: "un solo registro de tools; ninguna se duplica en
-- versión worker"). Lo llenan las tools básicas (init.lua) y cualquier extensión
-- (MCP en S41) con `agent.tool`.
local tools = {}

-- agent.tool{ name, description, schema, handler, permissions? } (agente.md §3).
-- Registra una tool. `handler(args, ctx) ⏸ -> string|Block[]|tabla`. Un
-- re-registro del mismo nombre lo SUSTITUYE (un plugin puede pisar una oficial).
-- `permissions.default` ("ask"|"allow"|"deny") fija la política base de la tool:
-- las de solo lectura se registran con "allow" (agente.md §5 amortiguador 1).
function M.tool(spec)
  if type(spec) ~= "table" then
    einval("agent.tool espera una tabla { name, description, schema, handler, permissions? }")
  end
  if type(spec.name) ~= "string" or spec.name == "" then
    einval("agent.tool: `name` debe ser una cadena no vacía")
  end
  if type(spec.handler) ~= "function" then
    einval(string.format("agent.tool %q: `handler` debe ser una función (args, ctx) -> resultado", spec.name))
  end
  local default = "ask"
  if spec.permissions ~= nil then
    if type(spec.permissions) ~= "table" then
      einval(string.format("agent.tool %q: `permissions` debe ser una tabla { default = ... }", spec.name))
    end
    if spec.permissions.default ~= nil then
      local d = spec.permissions.default
      if d ~= "ask" and d ~= "allow" and d ~= "deny" then
        einval(string.format("agent.tool %q: permissions.default debe ser \"ask\", \"allow\" o \"deny\"", spec.name))
      end
      default = d
    end
  end
  tools[spec.name] = {
    name        = spec.name,
    description = spec.description or "",
    schema      = spec.schema or { type = "object" },
    handler     = spec.handler,
    default     = default,
  }
end

-- M.tools() -> {name, description, schema}[] enumera las tools registradas (para
-- ensamblar el request, §7, y para introspección). Copia defensiva (sin handler).
function M.tools()
  local out = {}
  for _, t in pairs(tools) do
    out[#out + 1] = { name = t.name, description = t.description, schema = t.schema }
  end
  return out
end

-- ---------------------------------------------------------------------------
-- HOOKS-MIDDLEWARE (agente.md §4): registro PROPIO, NO el bus de eventos.
-- ---------------------------------------------------------------------------

-- Puntos de hook v1 (agente.md §4): cada uno con una lista de { fn, priority,
-- seq } ordenable. `request.pre` muta el request; `tool.pre` veta/reescribe args;
-- `tool.post` reescribe el resultado; `permission` concede/deniega; `compact`.
local HOOK_POINTS = {
  ["request.pre"] = true,
  ["tool.pre"]    = true,
  ["tool.post"]   = true,
  ["permission"]  = true,
  ["compact"]     = true,
}

local hooks = {}        -- point -> { {fn, priority, seq, live}, ... }
local hook_seq = 0      -- desempate estable: orden de registro

-- agent.hook(point, fn, opts?) -> Hook (agente.md §4). Registra un middleware.
-- `fn(payload, ctx)` devuelve nil (no opina), un payload sustituto (sigue con él)
-- o `{ deny = "razón" }` (corta la cadena; el primer deny gana). Orden: priority
-- ascendente, luego orden de registro. `Hook:remove()` lo desregistra.
function M.hook(point, fn, opts)
  if not HOOK_POINTS[point] then
    einval(string.format("agent.hook: punto %q desconocido (v1: request.pre, tool.pre, tool.post, permission, compact)", tostring(point)))
  end
  if type(fn) ~= "function" then
    einval("agent.hook: el segundo argumento debe ser una función (payload, ctx) -> nil|payload|{deny}")
  end
  opts = opts or {}
  hook_seq = hook_seq + 1
  local entry = { fn = fn, priority = opts.priority or 0, seq = hook_seq, live = true }
  hooks[point] = hooks[point] or {}
  table.insert(hooks[point], entry)
  return {
    remove = function()
      entry.live = false
    end,
  }
end

-- run_hooks(point, payload, ctx) corre la cadena de middleware de `point` sobre
-- `payload` (agente.md §4). Devuelve:
--   - el payload (posiblemente sustituido por algún hook) si nadie deniega;
--   - nil + razón si algún hook devolvió `{ deny = "razón" }` (el PRIMER deny
--     gana y corta la cadena).
-- Cada hook corre bajo `pcall` (frontera robusta, ADR-008): un hook que lanza se
-- loguea y se ignora (no opina), la cadena sigue. Itera sobre una COPIA ordenada
-- por (priority, seq) tomada al entrar (cancelar a mitad no rompe el recorrido;
-- los `live=false` se saltan).
local function run_hooks(point, payload, ctx)
  local list = hooks[point]
  if not list or #list == 0 then
    return payload, nil
  end
  local ordered = {}
  for _, e in ipairs(list) do
    if e.live then
      ordered[#ordered + 1] = e
    end
  end
  table.sort(ordered, function(a, b)
    if a.priority ~= b.priority then
      return a.priority < b.priority
    end
    return a.seq < b.seq
  end)
  for _, e in ipairs(ordered) do
    if e.live then
      local ok, res = pcall(e.fn, payload, ctx)
      if not ok then
        nu.log.warn("agent: hook %q lanzó y se ignora: %s", point,
          (type(res) == "table" and res.message) or tostring(res))
      elseif type(res) == "table" and res.deny ~= nil then
        return nil, tostring(res.deny)
      elseif res ~= nil then
        payload = res -- sustituye y sigue
      end
    end
  end
  return payload, nil
end

-- Limpieza del registro de hooks (para tests deterministas y `reload`). No es
-- parte del contrato público pero es inofensivo y útil.
function M._reset_hooks()
  hooks = {}
  hook_seq = 0
end

-- ---------------------------------------------------------------------------
-- Paquetes de caps con nombre (agente.md §9): vocabulario de ESTA extensión.
-- ---------------------------------------------------------------------------

-- Tablas Lua normales e inspeccionables (agente.md §9): el vocabulario de
-- permisos-duros (caps de worker, G6) vive aquí; el mecanismo (sandbox por caps)
-- en el core. Los subagentes (S40) las usarán para recortar la API de su worker.
M.caps = {
  FS_RO   = { "fs.read", "fs.stat", "fs.list", "fs.cwd" },
  FS_RW   = { "fs" },
  SEARCH  = { "search" },
  NET     = { "http", "ws" },
}

-- ---------------------------------------------------------------------------
-- PERMISOS (agente.md §5).
-- ---------------------------------------------------------------------------

-- match_pattern(pattern, tool_name, arg_text) -> bool. Un patrón es
-- `tool` o `tool:argumento` (agente.md §5). El `argumento` admite el comodín `*`
-- (glob simple: `bash:git *` casa `git status`). Se compara contra una
-- representación textual de los args (`arg_text`). Sin `:` el patrón casa
-- cualquier invocación de esa tool.
local function glob_to_pattern(glob)
  -- Escapa los mágicos de Lua salvo `*`, que pasa a `.*` (glob → patrón Lua).
  local out = glob:gsub("[%^%$%(%)%%%.%[%]%+%-%?]", "%%%1"):gsub("%*", ".*")
  return "^" .. out .. "$"
end

local function match_pattern(pattern, tool_name, arg_text)
  local colon = pattern:find(":", 1, true)
  if not colon then
    return pattern == tool_name
  end
  local p_tool = pattern:sub(1, colon - 1)
  if p_tool ~= tool_name then
    return false
  end
  local p_arg = pattern:sub(colon + 1)
  if not p_arg:find("*", 1, true) then
    return p_arg == (arg_text or "")
  end
  return (arg_text or ""):match(glob_to_pattern(p_arg)) ~= nil
end

local function matches_any(list, tool_name, arg_text)
  for _, pat in ipairs(list or {}) do
    if match_pattern(pat, tool_name, arg_text) then
      return pat
    end
  end
  return nil
end

-- arg_text(tool_name, args) -> string. Representación textual de los args para
-- casar patrones `tool:argumento` (agente.md §5). Para una tool `bash` el
-- argumento natural es el comando; para el resto, un campo `path`/`command`/`cmd`
-- si lo hay, o vacío. Es heurístico pero suficiente para los patrones v1.
local function arg_text(tool_name, args)
  if type(args) ~= "table" then
    return ""
  end
  return tostring(args.command or args.cmd or args.path or args.file or "")
end

-- pending_asks: asks pendientes por id, cada uno con su `future` (G3: varias
-- sesiones pueden tener asks a la vez; cada una espera SIN timeout). La UI/chat
-- responde con `agent.permission.respond(id, granted)`.
local pending_asks = {}
local ask_seq = 0

M.permission = {}

-- agent.permission.respond(id, granted) responde a un ask pendiente (agente.md
-- §5). `granted` true concede, false/nil deniega. Lo llama la UI (chat, S43) tras
-- pintar el diálogo de `agent:permission.asked`. Sin id válido → no-op silencioso
-- (el ask pudo expirar al cancelarse el turno).
function M.permission.respond(id, granted)
  local p = pending_asks[id]
  if p == nil then
    return
  end
  pending_asks[id] = nil
  p.future:set(granted == true)
end

-- check_permission(session, tool, args) decide si una tool call puede ejecutarse
-- (agente.md §5). Pipeline:
--   1. la tool de solo lectura con default="allow" se concede directa (amortig. 1);
--   2. `deny` de la política (corta): denegado;
--   3. `allow` de la política (concede);
--   4. hooks `permission` (pueden conceder/denegar programáticamente);
--   5. nadie decidió:
--        - default de la tool = "deny" → denegado;
--        - mode = "auto" → concedido (explícito y ruidoso, amortiguador 3);
--        - mode = "ask" Y hay UI (`nu.has("ui")`, G20) → se emite
--          `agent:permission.asked` y se ESPERA la respuesta (future, sin timeout);
--        - mode = "ask" SIN UI (headless, CI) → DEFAULT DENY (agente.md §5).
-- Devuelve true (concedido) o (false, razon_accionable). La razón nombra el
-- patrón EXACTO a añadir (amortiguador 2): "denegado `bash:npm install`; añade
-- allow = {\"bash:npm *\"}".
local function check_permission(session, tool, args)
  local perms = session.permissions
  local name = tool.name
  local atext = arg_text(name, args)

  -- 1. Solo lectura declarada: nunca pide (agente.md §5 amortiguador 1).
  if tool.default == "allow" then
    return true
  end
  -- Una tool con default="deny" se deniega salvo allow explícito (se evalúa abajo).

  -- 2. deny de la política corta (agente.md §5: deny → allow → hooks).
  local denied = matches_any(perms.deny, name, atext)
  if denied then
    return false, string.format("permiso denegado por `deny = {%q}` para la tool %q", denied, name)
  end

  -- 3. allow de la política concede.
  if matches_any(perms.allow, name, atext) then
    return true
  end

  -- 4. hooks `permission` (conceden/deniegan programáticamente). El payload lleva
  -- la tool y los args; un hook devuelve `{ deny = razon }` para denegar, o un
  -- payload con `grant = true` para conceder.
  local payload, deny_reason = run_hooks("permission",
    { tool = name, args = args, arg_text = atext }, { session = session.handle })
  if deny_reason ~= nil then
    return false, string.format("permiso denegado por un hook `permission`: %s", deny_reason)
  end
  if type(payload) == "table" and payload.grant == true then
    return true
  end

  -- 5. Nadie decidió. El patrón ACCIONABLE a añadir (amortiguador 2).
  local suggested = name
  if atext ~= "" then
    suggested = name .. ":" .. atext
  end
  local action = string.format("denegado %q; concédelo con allow = {%q} (o ejecuta con --auto-permissions)", name, suggested)

  if tool.default == "deny" then
    return false, "la tool está registrada con default = \"deny\"; " .. action
  end

  if perms.mode == "auto" then
    return true -- modo auto explícito y ruidoso (amortiguador 3)
  end

  -- mode = "ask". En headless (sin UI, G20) no hay quien responda: default DENY.
  if not nu.has("ui") then
    return false, "permiso requerido en modo headless (sin UI): " .. action
  end

  -- Hay UI: se pregunta y se ESPERA la respuesta (future, sin timeout, G3).
  ask_seq = ask_seq + 1
  local id = "ask-" .. session.handle.id .. "-" .. ask_seq
  local fut = nu.task.future()
  pending_asks[id] = { future = fut, session = session.handle.id }
  emit(session.handle.id, "permission.asked", { id = id, tool = name, args = args, suggested = suggested })
  local granted = fut:await()
  if granted then
    return true
  end
  return false, "permiso denegado por el usuario: " .. action
end

-- ---------------------------------------------------------------------------
-- Configuración (agente.md §10).
-- ---------------------------------------------------------------------------

-- load_config() -> tabla lee `config.dir()/agent.toml` (agente.md §10) de forma
-- perezosa y cacheada. Ausente → defaults. Mal formado → EAGENT accionable. Solo
-- lee los campos v1 que esta sesión usa: `model`, `max_turns`, `permissions`.
local config_cache = nil
local function load_config()
  if config_cache ~= nil then
    return config_cache
  end
  local path = nu.config.dir() .. "/agent.toml"
  local ok, raw = pcall(nu.fs.read, path)
  if not ok then
    if type(raw) == "table" and raw.code == "ENOENT" then
      config_cache = {}
      return config_cache
    end
    error(raw)
  end
  local okd, decoded = pcall(nu.toml.decode, raw)
  if not okd then
    eagent(string.format("agent.toml mal formado (%s): %s", path,
      (type(decoded) == "table" and decoded.message) or tostring(decoded)))
  end
  config_cache = decoded or {}
  return config_cache
end

-- M.reload_config() invalida la caché de `agent.toml`.
function M.reload_config()
  config_cache = nil
end

-- normalize_permissions(opts_perms, cfg) -> Permissions. Combina los permisos de
-- la sesión (`opts.permissions`) con los globales de `agent.toml` (agente.md §10:
-- defaults < global < sesión). El repo solo recorta (agente.md §11) — fuera del
-- alcance de S39, que no lee `.nu/agent.toml`; se documenta para S45.
local function normalize_permissions(opts_perms, cfg)
  local global = (cfg and cfg.permissions) or {}
  local sess = opts_perms or {}
  local function concat(a, b)
    local out = {}
    for _, v in ipairs(a or {}) do out[#out + 1] = v end
    for _, v in ipairs(b or {}) do out[#out + 1] = v end
    return out
  end
  return {
    mode  = sess.mode or global.mode or "ask",
    allow = concat(global.allow, sess.allow),
    deny  = concat(global.deny, sess.deny),
  }
end

-- ---------------------------------------------------------------------------
-- System prompt (agente.md §7).
-- ---------------------------------------------------------------------------

-- assemble_system(opts) -> string|nil. Ensambla el system prompt por piezas
-- ordenadas (agente.md §7): base de la extensión → (índice de skills, S39 no las
-- carga) → `nu.md` del repo si existe y es de confianza (TOFU §11, fuera de S39)
-- → `opts.system`. En S39 las piezas v1 son la base y `opts.system`; el resto
-- son extensiones posteriores. Devuelve nil si no hay nada (request sin system).
local BASE_SYSTEM = "Eres un agente de codificación que opera sobre un repositorio mediante tools."

local function assemble_system(opts)
  local parts = {}
  if opts.no_base ~= true then
    parts[#parts + 1] = BASE_SYSTEM
  end
  if type(opts.system) == "string" and opts.system ~= "" then
    parts[#parts + 1] = opts.system
  end
  if #parts == 0 then
    return nil
  end
  return table.concat(parts, "\n\n")
end

-- ---------------------------------------------------------------------------
-- El handle Session y el TURNO (agente.md §2).
-- ---------------------------------------------------------------------------

local Session = {}
Session.__index = Session

-- Session.usage / Session.id se exponen como campos (agente.md §2). `usage` se
-- actualiza al cerrar cada turno con el `usage` del proveedor.

-- run_tool(session, call) ejecuta UNA tool call (agente.md §2 paso 5): permisos →
-- tool.pre → handler → tool.post → tool_result. Devuelve el bloque `tool_result`
-- canónico (providers.md §2.2) a anexar al historial. Un error en cualquier punto
-- (permiso denegado, handler que lanza, deny de hook) NO rompe el loop: produce un
-- `tool_result` con `is_error = true` y el texto accionable, que el modelo VE
-- (agente.md §3) y puede corregir.
local function run_tool(session, call)
  local sid = session.handle.id
  local tool = tools[call.name]

  local function err_result(text)
    emit(sid, "tool.end", { id = call.id, name = call.name, is_error = true, error = text })
    return {
      type = "tool_result",
      id = call.id,
      content = { { type = "text", text = text } },
      is_error = true,
    }
  end

  if tool == nil then
    return err_result(string.format("tool desconocida: %q (no está registrada)", tostring(call.name)))
  end

  emit(sid, "tool.start", { id = call.id, name = call.name, args = call.args })

  -- Permisos (agente.md §5). Denegar produce un error ACCIONABLE devuelto al
  -- modelo como tool_result is_error (el turno no se rompe).
  local granted, reason = check_permission(session, tool, call.args)
  if not granted then
    return err_result(reason)
  end

  -- Hooks tool.pre (vetar / reescribir args, agente.md §4).
  local pre_payload, deny_reason = run_hooks("tool.pre",
    { tool = call.name, args = call.args, id = call.id }, { session = session.handle })
  if deny_reason ~= nil then
    return err_result(string.format("la tool %q fue vetada por un hook tool.pre: %s", call.name, deny_reason))
  end
  local args = (type(pre_payload) == "table" and pre_payload.args) or call.args

  -- ctx del handler (agente.md §3): session, cwd, progress, ask.
  local ctx = {
    session = session.handle,
    cwd = session.cwd,
    progress = function(text)
      emit(sid, "tool.progress", { id = call.id, name = call.name, text = tostring(text) })
    end,
    ask = function(question)
      -- ask del handler: reusa el flujo de permisos en su versión genérica. En
      -- headless sin UI no hay respuesta → false (coherente con §5 default deny).
      if not nu.has("ui") then
        return false
      end
      ask_seq = ask_seq + 1
      local id = "ask-" .. sid .. "-" .. ask_seq
      local fut = nu.task.future()
      pending_asks[id] = { future = fut, session = sid }
      emit(sid, "permission.asked", { id = id, tool = call.name, question = tostring(question) })
      return fut:await()
    end,
  }

  -- Handler (corre como parte de la task del turno; puede suspender, agente.md
  -- §3). Un error lanzado → tool_result is_error (el modelo lo ve).
  local ok, result = pcall(tool.handler, args, ctx)
  if not ok then
    local msg = (type(result) == "table" and result.message) or tostring(result)
    return err_result(string.format("la tool %q falló: %s", call.name, msg))
  end

  -- Hooks tool.post (reescribir el resultado, agente.md §4).
  local post_payload = run_hooks("tool.post",
    { tool = call.name, args = args, id = call.id, result = result }, { session = session.handle })
  if type(post_payload) == "table" and post_payload.result ~= nil then
    result = post_payload.result
  end

  -- Normaliza el resultado del handler a content: Block[] (providers.md §2.2). Un
  -- string → un bloque de texto; una tabla con `type` → un bloque; un array de
  -- bloques se usa tal cual.
  local content
  if type(result) == "string" then
    content = { { type = "text", text = result } }
  elseif type(result) == "table" and result.type ~= nil then
    content = { result }
  elseif type(result) == "table" then
    content = result -- se asume Block[]
  else
    content = { { type = "text", text = tostring(result) } }
  end

  emit(sid, "tool.end", { id = call.id, name = call.name, is_error = false })
  return { type = "tool_result", id = call.id, content = content }
end

-- consume_stream(session, iter) consume el iterador de Events del adaptador
-- (providers.md §2.3), re-emitiendo los deltas en el bus (`agent:delta`) para
-- quien pinte, y devuelve el `done` (con `stop_reason` y el `Message` ensamblado).
-- El agente NO re-ensambla deltas: el `done` trae el Message completo (§2.3).
local function consume_stream(session, iter)
  local sid = session.handle.id
  local done = nil
  local usage = nil
  for ev in iter do
    if ev.type == "done" then
      done = ev
    elseif ev.type == "usage" then
      usage = ev
      emit(sid, "delta", { kind = "usage", input_tokens = ev.input_tokens,
        output_tokens = ev.output_tokens, cache_read_tokens = ev.cache_read_tokens })
    else
      -- text / thinking / tool_call.* : se re-emiten crudos para la UI en vivo.
      emit(sid, "delta", ev)
    end
  end
  if done == nil then
    eagent("el adaptador cerró el stream sin un evento `done` (providers.md §2.3 lo exige)")
  end
  done._usage = usage
  return done
end

-- Session:send(content) ⏸ -> Message (agente.md §2). EL TURNO COMPLETO.
function Session:send(content)
  if self.closed then
    eagent("la sesión está cerrada")
  end

  -- Mensaje del usuario (agente.md §2 paso 1): string → un bloque de texto.
  local user_blocks
  if type(content) == "string" then
    user_blocks = { { type = "text", text = content } }
  elseif type(content) == "table" then
    user_blocks = content
  else
    einval("Session:send espera un string o un array de bloques (providers.md §2.2)")
  end
  local user_message = { role = "user", content = user_blocks }
  table.insert(self.history, user_message)
  if self.store then
    self.store:append_message(user_message)
  end

  emit(self.handle.id, "turn.start", {})

  local resolved = providers.resolve(self.model)
  local adapter = resolved.adapter
  local provider_config = resolved.config

  local final_message = nil
  local turns = 0

  while true do
    turns = turns + 1
    if turns > self.max_turns then
      emit(self.handle.id, "error", { message = "max_turns agotado", max_turns = self.max_turns })
      eagent(string.format("se agotó max_turns (%d) sin que el modelo terminara", self.max_turns),
        { reason = "max_turns" })
    end

    -- Ensambla el request canónico (agente.md §7) y pásalo por request.pre (§4).
    local request = {
      model       = provider_config.model.id,
      system      = assemble_system(self.opts),
      messages    = self.history,
      tools       = self.tools_for_request,
      max_tokens  = self.max_tokens,
      temperature = self.temperature,
    }
    local hooked, deny_reason = run_hooks("request.pre", request, { session = self.handle })
    if deny_reason ~= nil then
      eagent("el request fue vetado por un hook request.pre: " .. deny_reason)
    end
    request = hooked or request

    -- Llama al adaptador y consume el stream (agente.md §2 pasos 3-4).
    local iter = adapter.stream(request, provider_config)
    local done = consume_stream(self, iter)

    local assistant = done.message
    -- Persiste el mensaje del assistant con usage/modelo (agente.md §2 paso 4;
    -- sesiones.md §3): el coste y el llenado de contexto se auditan leyendo el JSONL.
    local usage = done._usage
    table.insert(self.history, assistant)
    if self.store then
      self.store:append_message(assistant, { usage = usage, model = self.model })
    end
    if usage ~= nil then
      self.usage.context_tokens = usage.input_tokens or self.usage.context_tokens
      -- last_usage: el usage del proveedor del ÚLTIMO turno (input/output_tokens),
      -- distinto de `self.usage` (acumulado de la sesión). Lo usa el digesto de un
      -- subagente en modo task (§9) para alinear su forma con la del modo worker.
      self.last_usage = usage
    end
    self.usage.turns = self.usage.turns + 1
    emit(self.handle.id, "message", { message = assistant, usage = usage, stop_reason = done.stop_reason })
    final_message = assistant

    -- ¿Hay tool calls? (agente.md §2 paso 5). Si no, el turno termina.
    if done.stop_reason ~= "tool_calls" then
      break
    end

    -- Ejecuta cada tool call EN ORDEN (P12: la paralela está pospuesta) y anexa
    -- los tool_result como un mensaje de usuario (providers.md §2.2: los
    -- tool_result viajan en un mensaje rol user). Luego vuelve al paso 2.
    local results = {}
    for _, block in ipairs(assistant.content) do
      if block.type == "tool_call" then
        results[#results + 1] = run_tool(self, block)
      end
    end
    if #results == 0 then
      -- stop_reason=tool_calls pero el mensaje no trae bloques tool_call: el
      -- modelo se contradijo; terminamos para no hacer loop vacío.
      break
    end
    local tool_message = { role = "user", content = results }
    table.insert(self.history, tool_message)
    if self.store then
      self.store:append_message(tool_message)
    end
    -- vuelve al while (re-pide al provider con los resultados).
  end

  emit(self.handle.id, "turn.end", { message = final_message })
  return final_message
end

-- Session:spawn(opts) -> Sub (agente.md §9). Lanza un SUBAGENTE: un agente que
-- corre AISLADO y devuelve a este (su padre) un resultado DIGERIDO. Delega en el
-- módulo `subagent` (cableado al final de este fichero con `subagent.attach(M)`).
-- opts = los de agent.session + { worker? = false, caps?: string[] }.
function Session:spawn(opts)
  if self.closed then
    eagent("la sesión está cerrada")
  end
  return M._subagent.spawn(self, opts)
end

-- M.run_tool_proxy(session, call) -> tool_result. Corre UNA tool por el pipeline
-- COMPLETO (permisos → hooks → handler → tool_result) sobre `session` (agente.md
-- §9). Es lo que el PADRE invoca cuando un subagente-worker proxya una tool: la
-- ejecución ocurre SIEMPRE en el estado principal, bajo el pipeline centralizado
-- (el worker no puede saltárselo). Devuelve el bloque tool_result canónico
-- (JSON-able: cruza al worker). Reusa `run_tool`, idéntico al del turno (§2 paso 5).
function M.run_tool_proxy(session, call)
  return run_tool(session, call)
end

-- Session:set_model(model) cambio en caliente (agente.md §2 G19). Valida contra el
-- registro de providers (resolve lanza si no existe) y aplica desde el siguiente
-- request. Escribe una entrada `event` en el transcript (sesiones.md §3).
function Session:set_model(model)
  if type(model) ~= "string" or model == "" then
    einval("Session:set_model espera \"proveedor/modelo\"")
  end
  providers.resolve(model) -- valida; lanza EPROVIDER si no existe
  self.model = model
  if self.store then
    self.store:append({ t = "event", ns = "agent", data = { kind = "set_model", model = model } })
  end
end

-- Session:close() libera la sesión de almacenamiento (suelta el lock, sesiones.md
-- §6). Idempotente.
function Session:close()
  if self.closed then
    return
  end
  self.closed = true
  emit(self.handle.id, "session.end", {})
  if self.store then
    self.store:close()
  end
end

-- ---------------------------------------------------------------------------
-- agent.session (agente.md §2).
-- ---------------------------------------------------------------------------

-- agent.session(opts) -> Session (agente.md §2). Crea o reanuda una sesión. opts:
--   - model (string, requerido salvo agent.toml `model`): "proveedor/modelo".
--   - system?, cwd?, tools?: string[], permissions?, resume?, max_turns?, etc.
--   - no_store? (S39): NO persistir (test in-memory). Por defecto persiste.
-- Persiste vía la extensión `sessions` (S38): crea/reanuda el transcript JSONL y
-- adquiere el lock de escritor. Reanudar (resume) hace replay del transcript y
-- repuebla el historial en memoria (agente.md §2 G18).
function M.session(opts)
  opts = opts or {}
  if type(opts) ~= "table" then
    einval("agent.session espera una tabla de opciones")
  end

  local cfg = load_config()
  local model = opts.model or cfg.model
  if type(model) ~= "string" or model == "" then
    einval("agent.session requiere `model` (\"proveedor/modelo\") en opts o en agent.toml")
  end
  -- Valida el modelo pronto (resolve lanza EPROVIDER si el provider/adaptador no
  -- existe): mejor fallar al abrir que en el primer turno.
  providers.resolve(model)

  local cwd = opts.cwd or nu.fs.cwd()

  -- Almacenamiento (agente.md §2 paso 1; sesiones.md). En S39 se persiste salvo
  -- `no_store` (tests in-memory). Reanudar pasa `resume` a sessions.open.
  local store = nil
  if opts.no_store ~= true then
    store = sessions.open({
      cwd     = cwd,
      resume  = opts.resume,
      parent  = opts.parent,
    })
  end

  -- tools_for_request: las ToolDef (name/description/schema) que se pasan al
  -- provider (agente.md §3). `opts.tools` (string[]) limita el conjunto; sin él,
  -- todas las registradas. Si la lista queda vacía, no se pasan tools.
  local tools_for_request = nil
  do
    local available = {}
    for _, t in pairs(tools) do
      available[t.name] = t
    end
    local chosen = {}
    if type(opts.tools) == "table" then
      for _, tn in ipairs(opts.tools) do
        if available[tn] then
          chosen[#chosen + 1] = available[tn]
        end
      end
    else
      for _, t in pairs(available) do
        chosen[#chosen + 1] = t
      end
    end
    if #chosen > 0 then
      tools_for_request = {}
      for _, t in ipairs(chosen) do
        tools_for_request[#tools_for_request + 1] =
          { name = t.name, description = t.description, schema = t.schema }
      end
    end
  end

  local self = setmetatable({
    model            = model,
    opts             = opts,
    cwd              = cwd,
    store            = store,
    history          = {},
    permissions      = normalize_permissions(opts.permissions, cfg),
    max_turns        = opts.max_turns or cfg.max_turns or DEFAULT_MAX_TURNS,
    max_tokens       = opts.max_tokens,
    temperature      = opts.temperature,
    tools_for_request = tools_for_request,
    closed           = false,
    usage            = { context_tokens = 0, cost_usd = 0, turns = 0 },
  }, Session)

  -- El handle público expuesto a hooks/ctx/eventos: id + usage (agente.md §2).
  self.handle = {
    id = (store and store.id) or ("mem-" .. tostring(self)),
    usage = self.usage,
  }
  self.id = self.handle.id

  -- Reanudación (agente.md §2 G18): replay del transcript → repuebla el historial
  -- en memoria con los mensajes (la política de replay para el LLM —tomar el
  -- último compact y los message siguientes— vive aquí, no en la persistencia).
  if store and opts.resume then
    local entries = store:replay()
    local last_compact = 0
    for i, e in ipairs(entries) do
      if e.t == "compact" then
        last_compact = i
      end
    end
    for i = (last_compact > 0 and last_compact or 1), #entries do
      local e = entries[i]
      if e.t == "message" and type(e.message) == "table" then
        table.insert(self.history, e.message)
      elseif e.t == "compact" and type(e.summary) == "table" then
        table.insert(self.history, e.summary)
      end
    end
  end

  emit(self.handle.id, "session.start", { model = model })
  return self
end

-- ---------------------------------------------------------------------------
-- Subagentes (agente.md §9, S40). Se cablea al final, cuando `M` (con `session`,
-- `run_tool_proxy`, `caps`) ya está completo: `subagent.attach(M)` inyecta el
-- módulo `agent` en el de subagentes (evita un require circular) y expone
-- `M._subagent.spawn`, que usa `Session:spawn`.
-- ---------------------------------------------------------------------------
M._subagent = require("agent.subagent").attach(M)

return M
