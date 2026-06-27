-- Adaptador `anthropic` de la extensión `providers` (S37): el primer dialecto
-- REAL, sobre la red.
--
-- Cumple el **contrato del adaptador** de
-- [providers.md](../../../../../docs/providers.md) §3: una tabla con `name`,
-- `caps`, `stream(req, provider) -> iterator<Event>` (⏸) y `count_tokens?`. A
-- diferencia del STUB de S36 (eventos fijos, sin red), este TRADUCE:
--
--   1. la petición CANÓNICA (providers.md §2.1: `messages`, `system`, `tools`,
--      `max_tokens`, `temperature`, `thinking`) al cuerpo de la **Messages API**
--      de Anthropic, y
--   2. el **SSE del dialecto Anthropic** (`message_start`,
--      `content_block_start/delta/stop` con tipos `text`/`tool_use`/`thinking`,
--      `message_delta`, `message_stop`, `error`, `ping`) al **stream de Eventos
--      CANÓNICO** de providers.md §2.3 (`text`, `thinking`, `tool_call.begin`,
--      `tool_call.delta`, `tool_call.end`, `usage`, `done`).
--
-- Todo sobre la API pública (api.md, corolario de completitud): `nu.http.stream`
-- + `Stream:events()` (§8, el parser SSE ya entrega `{event, data, id}`),
-- `nu.json.encode/decode` (§12), `error` estructurado (ADR-009). NINGÚN
-- privilegio de kernel: es Lua puro sobre la superficie congelada (ADR-003).
--
-- Reusa el módulo público de S36: `register_adapter` lo registra desde el
-- `init.lua`, `approx_tokens` alimenta `count_tokens`, y `EPROVIDER` es el código
-- de error de la extensión.

local providers = require("providers")

-- Versión de la Messages API. Cabecera `anthropic-version` obligatoria.
local ANTHROPIC_VERSION = "2023-06-01"

-- Capacidades declaradas (providers.md §3 `caps`): Anthropic soporta tools,
-- imágenes (bloques `image`), thinking (extended thinking), system prompt y
-- emite `usage`. Un adaptador declara las de SU proveedor; la degradación
-- declarada (§3 obligación 5) se apoya en esto.
local M = {
  name = "anthropic",
  caps = { tools = true, images = true, thinking = true, system = true, usage = true },
}

-- ---------------------------------------------------------------------------
-- Errores estructurados del adaptador (EPROVIDER, providers.md §3 / ADR-009).
-- ---------------------------------------------------------------------------

-- eprovider lanza el error del contrato (§3 obligación 2): `EPROVIDER` con
-- `detail = { status?, provider_code?, retryable }`. Marcar `retryable` es la
-- ÚNICA inteligencia de fallos que pide el contrato; el loop del agente decide
-- el reintento (§3 obligación 3: el adaptador no reintenta).
local function eprovider(message, detail)
  error({ code = "EPROVIDER", message = message, detail = detail })
end

local function einval(message)
  error({ code = "EINVAL", message = message })
end

-- ---------------------------------------------------------------------------
-- Traducción CANÓNICO -> dialecto Anthropic (request).
-- ---------------------------------------------------------------------------

-- canon_block_to_wire(block) -> tabla. Traduce UN bloque de contenido canónico
-- (providers.md §2.2) al formato de bloque de la Messages API. Preserva `meta`
-- como campo opaco del adaptador (providers.md §2.2 "regla meta", §3 obligación
-- 4 round-trip fiel): lo que llegó en `meta` —firmas de thinking, cache_control,
-- ids internos— se reinyecta tal cual en el wire.
local function canon_block_to_wire(block)
  local t = block.type
  local out

  if t == "text" then
    out = { type = "text", text = block.text }
  elseif t == "image" then
    -- Anthropic: bloque `image` con `source` base64 (providers.md §2.2).
    out = {
      type = "image",
      source = { type = "base64", media_type = block.media_type, data = block.data_base64 },
    }
  elseif t == "thinking" then
    -- El texto del razonamiento; la FIRMA de thinking viaja en `meta` (la pone
    -- el adaptador al ensamblar) y se reinyecta abajo por la regla meta.
    out = { type = "thinking", thinking = block.text }
  elseif t == "tool_call" then
    -- Canónico `tool_call {id,name,args}` -> Anthropic `tool_use {id,name,input}`.
    out = { type = "tool_use", id = block.id, name = block.name, input = block.args or {} }
  elseif t == "tool_result" then
    -- Canónico `tool_result {id,content,is_error?}` -> Anthropic `tool_result
    -- {tool_use_id, content, is_error?}`. El `content` es a su vez Block[].
    local content = {}
    for _, sub in ipairs(block.content or {}) do
      content[#content + 1] = canon_block_to_wire(sub)
    end
    out = { type = "tool_result", tool_use_id = block.id, content = content }
    if block.is_error ~= nil then
      out.is_error = block.is_error
    end
  else
    einval(string.format("adaptador anthropic: tipo de bloque desconocido %q (providers.md §2.2)", tostring(t)))
  end

  -- Round-trip fiel (providers.md §3 obligación 4 / regla meta §2.2): el `meta`
  -- opaco del adaptador se funde en el bloque del wire. Para thinking lleva la
  -- `signature`; para otros, `cache_control` u opacos del proveedor.
  if type(block.meta) == "table" then
    for k, v in pairs(block.meta) do
      out[k] = v
    end
  end

  return out
end

-- add_cache_control(t) marca un objeto del wire (una tool, un bloque de
-- contenido o un bloque de system) como breakpoint de caché de Anthropic
-- (`cache_control = {type="ephemeral"}`), SIN pisar uno que ya viniera por la
-- regla meta (round-trip fiel, §2.2): si el modelo canónico ya trajo su
-- `cache_control`, manda el suyo. Es el mecanismo de P31.
local function add_cache_control(t)
  if type(t) == "table" and t.cache_control == nil then
    t.cache_control = { type = "ephemeral" }
  end
end

-- to_wire(req, provider) -> tabla. Cuerpo de la Messages API a partir del
-- Request canónico (providers.md §2.1). `messages` -> `messages` (rol + bloques
-- traducidos), `system` -> campo `system` de Anthropic (cadena), `tools` ->
-- `tools` (name/description/input_schema), `max_tokens`/`temperature`/`thinking`.
local function to_wire(req, provider)
  local body = {
    model = req.model,
    -- `max_tokens` es OBLIGATORIO en Anthropic. Si el canónico no lo trae, cae
    -- al `max_output` del ModelInfo resuelto, y por último a un default seguro.
    max_tokens = req.max_tokens or (provider.model and provider.model.max_output) or 4096,
    stream = true,
  }

  -- System prompt al campo `system` de Anthropic (providers.md §2.1 -> dialecto).
  -- Forma de ARRAY de bloques (no string) para poder colgarle un breakpoint de
  -- caché (P31): Anthropic acepta `system` como `[{type="text", text, cache_control?}]`.
  if type(req.system) == "string" and req.system ~= "" then
    body.system = { { type = "text", text = req.system } }
  end

  -- Mensajes: rol + bloques traducidos.
  local messages = {}
  for _, msg in ipairs(req.messages or {}) do
    local blocks = {}
    for _, block in ipairs(msg.content or {}) do
      blocks[#blocks + 1] = canon_block_to_wire(block)
    end
    messages[#messages + 1] = { role = msg.role, content = blocks }
  end
  body.messages = messages

  -- Tools (providers.md §2.1: `{name, description, schema}`) -> Anthropic
  -- `{name, description, input_schema}`.
  if req.tools ~= nil and #req.tools > 0 then
    local tools = {}
    for _, tool in ipairs(req.tools) do
      tools[#tools + 1] = {
        name = tool.name,
        description = tool.description,
        input_schema = tool.schema or { type = "object" },
      }
    end
    body.tools = tools
  end

  if type(req.temperature) == "number" then
    body.temperature = req.temperature
  end

  -- Extended thinking (providers.md §2.1 `thinking = {budget?}`). Anthropic:
  -- `thinking = {type="enabled", budget_tokens=N}` para modelos que lo aceptan.
  --
  -- ⚠ PENDIENTE DE IMPLEMENTAR — ADR-016 / G34 (antes P21). La decisión YA está
  -- tomada en el contrato (providers.md §2.1): el canónico pasa a
  -- `thinking={ mode?, budget? }` y el dialecto de cada modelo es un DATO del
  -- providers.toml (`thinking = "adaptive"|"budget"|"none"`, en `provider.model`),
  -- que aquí habrá que leer para traducir POR-MODELO: `adaptive` →
  -- `{type="adaptive"}` (lo que espera Opus 4.6+, que retiró `budget_tokens` y
  -- 400ea con la forma legacy), `budget` → `{type="enabled", budget_tokens=N}`.
  -- Mientras esa sesión de construcción no llegue, se mantiene la traducción
  -- legacy (correcta para los modelos previos; sobre Opus 4.6+ daría 400, pero el
  -- agente no rellena `thinking` por defecto, así que está latente). Se acepta
  -- también `mode="budget"` como sinónimo de `budget` para no romper cuando el
  -- canónico nuevo empiece a usarse.
  if type(req.thinking) == "table" then
    local budget = req.thinking.budget
    if type(budget) == "number" then
      body.thinking = { type = "enabled", budget_tokens = budget }
    end
  end

  -- Prompt caching automático e invisible (providers.md §3 obligación 6, P31).
  -- Coloca los breakpoints `cache_control` MECÁNICAMENTE, sin que el modelo
  -- canónico ni el usuario indiquen nada: el prefijo estable (tools + system +
  -- el arranque de la conversación) se cachea y abarata los turnos siguientes.
  -- Anthropic admite hasta 4 breakpoints y cachea el prefijo hasta cada uno;
  -- por debajo del mínimo de tokens los ignora, así que marcar siempre es seguro.
  -- Estrategia:
  --   1. la ÚLTIMA tool (cachea todo el bloque de tools, que va primero);
  --   2. el system (su bloque de texto);
  --   3. los DOS últimos mensajes (su último bloque): captura el prefijo creciente
  --      de la conversación turno a turno (incremental caching).
  if body.tools and #body.tools > 0 then
    add_cache_control(body.tools[#body.tools])
  end
  if type(body.system) == "table" and #body.system > 0 then
    add_cache_control(body.system[#body.system])
  end
  do
    local n = #messages
    for _, mi in ipairs({ n, n - 1 }) do
      local m = messages[mi]
      if m and type(m.content) == "table" and #m.content > 0 then
        add_cache_control(m.content[#m.content])
      end
    end
  end

  return body
end

-- auth_headers(provider) -> tabla. Cabeceras de la Messages API: la clave va en
-- `x-api-key` (NO Bearer), más `anthropic-version` y el content-type.
local function auth_headers(provider)
  local h = {
    ["content-type"]      = "application/json",
    ["anthropic-version"] = ANTHROPIC_VERSION,
  }
  if type(provider.api_key) == "string" and provider.api_key ~= "" then
    h["x-api-key"] = provider.api_key
  end
  return h
end

-- ---------------------------------------------------------------------------
-- Traducción dialecto Anthropic SSE -> stream CANÓNICO de Eventos (providers.md
-- §2.3). El corazón de S37 (camino caliente).
-- ---------------------------------------------------------------------------

-- map_stop_reason(anthropic_reason) -> canónico (providers.md §2.3 `done`).
-- Anthropic: `end_turn`/`stop_sequence` -> "end"; `tool_use` -> "tool_calls";
-- `max_tokens` -> "max_tokens"; `refusal` -> "refusal".
local function map_stop_reason(reason)
  if reason == "tool_use" then
    return "tool_calls"
  elseif reason == "max_tokens" then
    return "max_tokens"
  elseif reason == "refusal" then
    return "refusal"
  end
  -- end_turn, stop_sequence, nil, desconocido -> "end".
  return "end"
end

-- make_iterator(stream, provider) -> función iteradora de Events. Consume el SSE
-- de Anthropic con `stream:events()` (api.md §8: ya parsea `event: <tipo>\ndata:
-- <json>\n\n` y entrega `{event, data, id}`), decodifica el `data` con
-- `nu.json.decode`, y mantiene la MÁQUINA DE ESTADOS del mensaje: bloques de
-- contenido por ÍNDICE, acumulando texto, razonamiento y el JSON de args de
-- tool_use (que llega troceado en `input_json_delta`), hasta `message_stop`.
--
-- Devuelve un iterador estilo Lua (una llamada -> un Event, `nil` al agotarse),
-- el mismo protocolo que consume `for ev in adapter.stream(...)` y que ya usaba
-- el stub (S36). Cada llamada hace AVANZAR el SSE lo justo para emitir el
-- siguiente Event canónico (puede consumir varios eventos Anthropic —p. ej.
-- `ping`— sin emitir nada hacia fuera).
local function make_iterator(stream, provider)
  local sse = stream:events() -- iterador ⏸ de {event, data, id} (api.md §8)

  -- Estado del mensaje ensamblado (providers.md §2.1, para el `done` final).
  local message = { role = "assistant", content = {} }
  local usage = { input_tokens = nil, output_tokens = nil, cache_read_tokens = nil }
  local stop_reason = "end"

  -- Bloques en construcción, indexados por el `index` de Anthropic. Cada uno:
  --   { type = "text"|"thinking"|"tool_use", text/acc, id, name, json_acc }
  local blocks = {}
  local finished = false  -- message_stop visto: ya solo queda emitir `done`.
  local done_emitted = false

  -- Cola de Events pendientes ya producidos pero aún no devueltos (un solo
  -- evento Anthropic puede no producir ninguno —ping— y el cierre produce
  -- varios —tool_call.end + ...—; una cola lo simplifica).
  local pending = {}
  local function enqueue(ev) pending[#pending + 1] = ev end

  -- finalize_block(idx): cierra el bloque idx, fijando su forma canónica final
  -- en `message.content` (providers.md §2.1) y, si es tool_use, decodificando el
  -- JSON de args acumulado a una tabla (`args`).
  local function finalize_block(idx)
    local b = blocks[idx]
    if b == nil then return end
    if b.type == "text" then
      message.content[#message.content + 1] = { type = "text", text = b.acc }
    elseif b.type == "thinking" then
      -- La FIRMA de thinking (signature_delta) es opaca del adaptador: viaja en
      -- `meta` para reinyectarla fiel en turnos siguientes (regla meta §2.2).
      local block = { type = "thinking", text = b.acc }
      if b.signature ~= nil then
        block.meta = { signature = b.signature }
      end
      message.content[#message.content + 1] = block
    elseif b.type == "tool_use" then
      -- El input llega troceado en `input_json_delta`; se acumula como TEXTO y
      -- se decodifica AHORA (al cerrar el bloque). JSON vacío -> tabla vacía.
      local args = {}
      if b.json_acc ~= nil and b.json_acc ~= "" then
        local ok, decoded = pcall(nu.json.decode, b.json_acc)
        if ok and type(decoded) == "table" then
          args = decoded
        end
        -- Un JSON de args mal formado no aborta el stream: el bloque canónico
        -- queda con args = {} (el agente lo verá; el adaptador no inventa).
      end
      message.content[#message.content + 1] =
        { type = "tool_call", id = b.id, name = b.name, args = args }
      enqueue({ type = "tool_call.end", id = b.id })
    end
    blocks[idx] = nil
  end

  -- handle(evt): traduce UN evento del SSE de Anthropic a 0+ Events canónicos
  -- encolados. `evt` es {event, data, id}; `data` es JSON sin decodificar.
  local function handle(evt)
    local kind = evt.event
    -- `ping` (y eventos sin nombre) no producen nada: keep-alive.
    if kind == nil or kind == "ping" then
      return
    end

    -- Errores del dialecto -> EPROVIDER (providers.md §3 obligación 2). 5xx y
    -- overloaded son retryables; 4xx no.
    if kind == "error" then
      local code, msg = "unknown", "error del proveedor (SSE)"
      local ok, payload = pcall(nu.json.decode, evt.data or "")
      if ok and type(payload) == "table" and type(payload.error) == "table" then
        code = payload.error.type or code
        msg = payload.error.message or msg
      end
      local retryable = (code == "overloaded_error" or code == "api_error")
      eprovider("anthropic: " .. msg, { provider_code = code, retryable = retryable })
    end

    -- A partir de aquí el evento lleva un `data` JSON que decodificamos.
    local ok, d = pcall(nu.json.decode, evt.data or "")
    if not ok or type(d) ~= "table" then
      -- Dato no decodificable en un evento conocido: lo ignoramos (robustez),
      -- como un comentario SSE.
      return
    end

    if kind == "message_start" then
      -- Trae el usage de entrada inicial (input_tokens, cache_read).
      if type(d.message) == "table" then
        if type(d.message.role) == "string" then
          message.role = d.message.role
        end
        if type(d.message.usage) == "table" then
          local u = d.message.usage
          usage.input_tokens = u.input_tokens or usage.input_tokens
          usage.cache_read_tokens = u.cache_read_input_tokens or usage.cache_read_tokens
          usage.output_tokens = u.output_tokens or usage.output_tokens
        end
      end
      -- Emitimos un `usage` temprano si trajo tokens de entrada (la UI lo usa
      -- para el llenado de contexto; providers.md §2.3).
      if usage.input_tokens ~= nil then
        enqueue({ type = "usage",
          input_tokens = usage.input_tokens,
          cache_read_tokens = usage.cache_read_tokens })
      end

    elseif kind == "content_block_start" then
      local idx = d.index
      local cb = d.content_block or {}
      if cb.type == "text" then
        blocks[idx] = { type = "text", acc = "" }
      elseif cb.type == "thinking" then
        blocks[idx] = { type = "thinking", acc = "", signature = nil }
      elseif cb.type == "tool_use" then
        blocks[idx] = { type = "tool_use", id = cb.id, name = cb.name, json_acc = "" }
        -- Comienzo de tool call canónico (providers.md §2.3).
        enqueue({ type = "tool_call.begin", id = cb.id, name = cb.name })
      else
        -- Tipo de bloque desconocido (forward-compat): lo registramos vacío para
        -- que content_block_stop no falle, pero no emitimos deltas.
        blocks[idx] = { type = "unknown" }
      end

    elseif kind == "content_block_delta" then
      local idx = d.index
      local b = blocks[idx]
      local delta = d.delta or {}
      if b ~= nil then
        if delta.type == "text_delta" then
          b.acc = (b.acc or "") .. (delta.text or "")
          enqueue({ type = "text", text = delta.text or "" })
        elseif delta.type == "thinking_delta" then
          b.acc = (b.acc or "") .. (delta.thinking or "")
          enqueue({ type = "thinking", text = delta.thinking or "" })
        elseif delta.type == "signature_delta" then
          -- La firma de thinking es opaca: se acumula para el `meta` del bloque.
          b.signature = (b.signature or "") .. (delta.signature or "")
        elseif delta.type == "input_json_delta" then
          -- Fragmento del JSON de args de un tool_use: SE ACUMULA como texto y
          -- se decodifica al cerrar (finalize_block). El delta se reexpone tal
          -- cual para que la UI pueda pintar el JSON en vivo (providers.md §2.3
          -- `tool_call.delta {id, args_json}`).
          b.json_acc = (b.json_acc or "") .. (delta.partial_json or "")
          enqueue({ type = "tool_call.delta", id = b.id, args_json = delta.partial_json or "" })
        end
      end

    elseif kind == "content_block_stop" then
      finalize_block(d.index)

    elseif kind == "message_delta" then
      -- Trae el stop_reason final y el usage de salida acumulado.
      if type(d.delta) == "table" and d.delta.stop_reason ~= nil then
        stop_reason = map_stop_reason(d.delta.stop_reason)
      end
      if type(d.usage) == "table" then
        usage.output_tokens = d.usage.output_tokens or usage.output_tokens
      end
      -- Emite el `usage` final (con output_tokens) para el contador del agente.
      enqueue({ type = "usage",
        input_tokens = usage.input_tokens,
        output_tokens = usage.output_tokens,
        cache_read_tokens = usage.cache_read_tokens })

    elseif kind == "message_stop" then
      finished = true
      -- Cierra cualquier bloque que quedara abierto (robustez ante un proveedor
      -- que no emita content_block_stop antes de message_stop).
      for idx in pairs(blocks) do
        finalize_block(idx)
      end
    end
  end

  -- El iterador propiamente: avanza el SSE hasta tener un Event que devolver, o
  -- hasta agotar el stream (y entonces el `done` final, una sola vez).
  return function()
    while true do
      if #pending > 0 then
        return table.remove(pending, 1)
      end
      if finished then
        -- Ya no quedan eventos pendientes y vimos message_stop: el `done` cierra
        -- el stream e incluye el Message ensamblado (providers.md §2.3).
        if done_emitted then
          return nil
        end
        done_emitted = true
        return { type = "done", stop_reason = stop_reason, message = message }
      end

      local evt = sse() -- ⏸: siguiente evento SSE, o nil si el stream terminó
      if evt == nil then
        -- El stream se cerró sin message_stop explícito: cerramos lo abierto y
        -- emitimos el `done` para no dejar al agente esperando.
        for idx in pairs(blocks) do
          finalize_block(idx)
        end
        finished = true
        -- vuelve al tope: drenará `pending` (tool_call.end pendientes) y luego
        -- el `done`.
      else
        handle(evt)
      end
    end
  end
end

-- ---------------------------------------------------------------------------
-- Contrato §3: stream + count_tokens.
-- ---------------------------------------------------------------------------

-- stream(req, provider) -> iterator<Event> ⏸ (providers.md §3). Traduce el
-- request, abre `nu.http.stream` (⏸, api.md §8), comprueba el status, y devuelve
-- el iterador que parsea el SSE de Anthropic al stream canónico. La cancelación
-- de la task del agente cierra el Stream subyacente (api.md §8 / §3 obligación 1).
function M.stream(req, provider)
  -- Degradación declarada (providers.md §3 obligación 5): este adaptador SÍ
  -- soporta tools (caps.tools=true), así que no rechaza; pero validamos lo
  -- mínimo del request para fallar pronto y accionable.
  if type(req) ~= "table" then
    einval("adaptador anthropic: el request debe ser una tabla (providers.md §2.1)")
  end
  if type(req.model) ~= "string" or req.model == "" then
    einval("adaptador anthropic: el request necesita `model` (id del proveedor, providers.md §2.1)")
  end

  local body = to_wire(req, provider)

  local stream = nu.http.stream({
    url = provider.base_url .. "/v1/messages",
    method = "POST",
    headers = auth_headers(provider),
    body = nu.json.encode(body),
  })

  -- El status >= 400 es DATO (api.md §8: `stream` no lanza por status), pero el
  -- adaptador SÍ lo convierte en EPROVIDER accionable (providers.md §3
  -- obligación 2). 429 y 5xx son retryables; el resto no.
  if stream.status ~= nil and stream.status >= 400 then
    local msg = "anthropic: HTTP " .. tostring(stream.status)
    local code = nil
    -- Cuerpo de error: en HTTP de error Anthropic suele mandar JSON, no SSE.
    -- Lo leemos con chunks() y lo decodificamos best-effort para el mensaje.
    local ok_chunks, raw = pcall(function()
      local acc = ""
      for chunk in stream:chunks() do
        acc = acc .. chunk
      end
      return acc
    end)
    if ok_chunks and raw ~= "" then
      local okj, payload = pcall(nu.json.decode, raw)
      if okj and type(payload) == "table" and type(payload.error) == "table" then
        code = payload.error.type
        if type(payload.error.message) == "string" then
          msg = "anthropic: " .. payload.error.message
        end
      end
    end
    stream:close()
    local retryable = (stream.status == 429 or stream.status >= 500)
    eprovider(msg, { status = stream.status, provider_code = code, retryable = retryable })
  end

  return make_iterator(stream, provider)
end

-- count_tokens(req, provider) -> integer ⏸ (providers.md §3, opcional). Anthropic
-- ofrece un endpoint exacto (`/v1/messages/count_tokens`), pero S37 usa la
-- heurística de la extensión (`approx_tokens`, G23) sobre system + bloques de
-- texto: es Lua puro, sin red, y suficiente para la estimación de llenado de
-- contexto previa a un turno (providers.md §5: la fuente de verdad es el `usage`
-- del propio turno; esto solo estima). El endpoint exacto queda como mejora.
function M.count_tokens(req, provider)
  local total = 0
  local at = providers.approx_tokens
  if type(req.system) == "string" then
    total = total + at(req.system)
  end
  for _, msg in ipairs(req.messages or {}) do
    for _, block in ipairs(msg.content or {}) do
      if block.type == "text" then
        total = total + at(block.text)
      elseif block.type == "thinking" then
        total = total + at(block.text)
      end
    end
  end
  return total
end

return M
