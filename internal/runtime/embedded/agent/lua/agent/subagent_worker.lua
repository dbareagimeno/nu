-- Bootstrap del LOOP de un subagente-worker (S40, agente.md §9).
--
-- Este módulo es el `module` que `nu.worker.spawn` carga DENTRO del worker para
-- correr el turno de un subagente AISLADO (agente.md §9: "el loop corre en un
-- worker, con caps recortadas; los handlers de tools se ejecutan en el estado
-- principal vía proxy de mensajes"). Es, por tanto, código que corre BAJO LAS CAPS
-- RECORTADAS del worker: la superficie no concedida NO EXISTE aquí (p. ej.
-- `nu.fs.write`, `nu.ui`, `nu.events` —el bus principal no cruza, api.md §13/§16—).
-- El subagente es HEADLESS por construcción.
--
-- PROTOCOLO con el padre (por `nu.worker.parent`, api.md §13, mensajes JSON-ables):
--   1. el padre manda `{ kind="init", model, system, prompt, tool_defs, adapters,
--      max_turns, max_tokens, temperature }`;
--   2. el worker ensambla el request canónico (agente.md §7) y consume el stream
--      del adaptador (providers.md §2.3) hasta el `done`;
--   3. si el modelo pidió tools, el worker NO las ejecuta: por cada tool_call manda
--      `{ kind="tool_call", id, name, args }` al padre y espera
--      `{ kind="tool_result", result }` —el padre la corrió por su pipeline
--      (permisos/hooks/handler) en el estado principal—; anexa el resultado y
--      RE-PIDE (vuelve al paso 2);
--   4. al terminar (modelo para sin tools, o se agota max_turns), manda el DIGESTO
--      `{ kind="done", digest = { text, message, stop_reason, usage, turns } }`.
--   Cualquier fallo se manda como `{ kind="error", message }` para que el padre lo
--   reporte como EAGENT (en vez de quedarse colgado esperando un digesto).
--
-- ¿POR QUÉ REGISTRAR ADAPTADORES AQUÍ? El `init.lua` de `providers` (que registra
-- los oficiales) NO corre dentro del worker —un worker solo ejecuta `require(module)`
-- (worker.go); no hay ciclo de vida de plugins, §13—. Así que el registro vivo de
-- adaptadores de `providers` arranca VACÍO en el worker; este bootstrap lo rellena
-- requiriendo los módulos de adaptador que el padre nombró en `init.adapters` (los
-- oficiales son require-ables: `providers.adapter_anthropic`). Es re-ejecutar lo que
-- haría init.lua, sin privilegio: Lua puro sobre la API pública.
--
-- ADR-003: Lua puro sobre la API pública [W] (api.md §16: task/json/toml/fs/...)
-- + el módulo `providers`. Sin privilegio de kernel.

local providers = require("providers")

-- Cada adaptador oficial expone un `name`; se registra bajo ese name para que
-- `providers.resolve` (que mira `provider.adapter` del providers.toml) lo encuentre.
local function register_adapters(modules)
  for _, modname in ipairs(modules or {}) do
    local ok, mod = pcall(require, modname)
    if ok and type(mod) == "table" and type(mod.name) == "string" then
      providers.register_adapter(mod.name, mod)
    end
    -- Un módulo de adaptador que no resuelve no es fatal aquí: si NINGUNO casa con el
    -- provider del modelo, `providers.resolve` fallará accionable más abajo (y se
    -- reporta como error al padre).
  end
end

-- text_of(message) -> string. El texto plano del mensaje final (para el digesto).
local function text_of(message)
  if type(message) ~= "table" or type(message.content) ~= "table" then
    return ""
  end
  local parts = {}
  for _, block in ipairs(message.content) do
    if block.type == "text" and type(block.text) == "string" then
      parts[#parts + 1] = block.text
    end
  end
  return table.concat(parts, "")
end

-- consume_stream(iter) -> done. Consume el iterador de Events del adaptador
-- (providers.md §2.3) y devuelve el `done` (con stop_reason y el Message ensamblado)
-- y el último `usage`. El subagente-worker es HEADLESS: no hay bus de eventos
-- (`nu.events` no existe en el worker, §16), así que los deltas se descartan —el
-- padre solo recibe el DIGESTO, no el stream crudo (agente.md §9)—.
local function consume_stream(iter)
  local done, usage = nil, nil
  for ev in iter do
    if ev.type == "done" then
      done = ev
    elseif ev.type == "usage" then
      usage = ev
    end
    -- text/thinking/tool_call.*: se descartan (headless, sin UI; sin stream crudo).
  end
  if done == nil then
    error({ code = "EAGENT", message = "el adaptador cerró el stream sin un evento `done`" })
  end
  return done, usage
end

-- run_turn(init) -> digest. El LOOP del subagente, idéntico en forma al de S39
-- (Session:send) pero con la ejecución de tools DELEGADA al padre por mensajes.
local function run_turn(init)
  local resolved = providers.resolve(init.model)
  local adapter = resolved.adapter
  local config = resolved.config

  -- Historial en memoria del subagente (su transcript propio vive en el padre,
  -- agente.md §9: "transcript propio como sesión hija"). Arranca con el prompt.
  local history = {}
  local prompt = init.prompt
  if type(prompt) == "string" then
    history[1] = { role = "user", content = { { type = "text", text = prompt } } }
  elseif type(prompt) == "table" then
    history[1] = { role = "user", content = prompt }
  end

  local max_turns = init.max_turns or 32
  local final_message, last_usage, last_stop = nil, nil, nil
  local turns = 0

  while true do
    turns = turns + 1
    if turns > max_turns then
      error({ code = "EAGENT", message = "se agotó max_turns en el subagente", detail = { reason = "max_turns" } })
    end

    local request = {
      model       = config.model.id,
      system      = init.system,
      messages    = history,
      tools       = (init.tool_defs and #init.tool_defs > 0) and init.tool_defs or nil,
      max_tokens  = init.max_tokens,
      temperature = init.temperature,
    }

    local iter = adapter.stream(request, config)
    local done, usage = consume_stream(iter)
    local assistant = done.message
    history[#history + 1] = assistant
    final_message, last_usage, last_stop = assistant, usage, done.stop_reason

    if done.stop_reason ~= "tool_calls" then
      break
    end

    -- Tools: el worker NO las ejecuta. Por cada tool_call, proxy al padre y espera
    -- su tool_result (agente.md §9: handlers en el estado principal). EN ORDEN (P12).
    local results = {}
    for _, block in ipairs(assistant.content) do
      if block.type == "tool_call" then
        nu.worker.parent.send({
          kind = "tool_call", id = block.id, name = block.name, args = block.args,
        })
        local reply = nu.worker.parent.recv()
        if reply == nil then
          error({ code = "EAGENT", message = "el padre cerró el canal sin devolver el tool_result" })
        end
        results[#results + 1] = reply.result
      end
    end
    if #results == 0 then
      break -- stop_reason=tool_calls sin bloques tool_call: no hacer loop vacío.
    end
    history[#history + 1] = { role = "user", content = results }
  end

  return {
    text        = text_of(final_message),
    message     = final_message,
    -- El motivo de parada REAL del último done (providers.md §2.3): el padre
    -- distingue así un final normal de un max_tokens o un refusal.
    stop_reason = last_stop,
    usage       = last_usage,
    turns       = turns,
  }
end

-- Cuerpo del worker (corre como task, así que puede ⏸): recibe el init, corre el
-- turno y devuelve el digesto. Un fallo se reporta como `error` para no colgar al
-- padre (que está en `w:recv()` esperando o un tool_result o el digesto).
local init = nu.worker.parent.recv()
if init == nil or init.kind ~= "init" then
  nu.worker.parent.send({ kind = "error", message = "primer mensaje no es un init" })
  return
end

register_adapters(init.adapters)

local ok, digest_or_err = pcall(run_turn, init)
if ok then
  nu.worker.parent.send({ kind = "done", digest = digest_or_err })
else
  local message = (type(digest_or_err) == "table" and digest_or_err.message) or tostring(digest_or_err)
  nu.worker.parent.send({ kind = "error", message = message })
end
