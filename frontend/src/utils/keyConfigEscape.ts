/**
 * Sanitizers for key-usage config generation.
 *
 * Model ids shown in "Use Key" snippets originate from upstream channel
 * pricing / gateway responses, so they must never be interpolated raw into
 * shell or TOML contexts: strip control characters, reject wildcard rules,
 * and keep media-only ids out of text-client defaults.
 */

/**
 * Ids that only ever existed in old hardcoded frontend tables (or backend
 * platform-default fallbacks) and must never be presented as a key's real
 * channel-effective model. Applied to server-derived lists only — an
 * explicitly provided parent list is trusted as-is.
 */
const MEDIA_MODEL_PATTERN = /(image|imagine|video|diffusion|tts|stt|whisper|dall-?e|imagen|\bveo\b|kling|runway|sora)/i

// Control-char handling without control-escape literals: the no-control-regex
// lint rule forbids them even inside RegExp strings, so compare char codes.
const fromCodes = (...codes: number[]): string => String.fromCharCode(...codes)
// All C0 controls except TAB(9), plus DEL(127): stripped from model ids.
const MODEL_STRIP_CHARS = fromCodes(0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 127)
// TOML basic strings can encode backspace/tab/LF/FF/CR, so only strip the rest.
const TOML_STRIP_CHARS = fromCodes(0, 1, 2, 3, 4, 5, 6, 7, 11, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 127)

function stripChars(value: string, stripped: string): string {
  let out = ''
  for (const ch of value) {
    if (!stripped.includes(ch)) out += ch
  }
  return out
}

/** Strip control characters (incl. CR/LF/NUL) and trim. Returns '' when unusable. */
export function sanitizeModelId(value: unknown): string {
  if (typeof value !== 'string') return ''
  return stripChars(value, MODEL_STRIP_CHARS).trim()
}

export function isMediaModelId(id: string): boolean {
  return MEDIA_MODEL_PATTERN.test(id)
}

export interface NormalizeModelIdsOptions {
  /** Drop wildcard rules (`*`). Default false. */
  dropWildcards?: boolean
}

/** Dedupe sanitized ids, preserving order. */
export function normalizeModelIds(values: unknown, options?: NormalizeModelIdsOptions): string[] {
  if (!Array.isArray(values)) return []
  const dropWildcards = options?.dropWildcards ?? false
  const ids: string[] = []
  for (const value of values) {
    const id = sanitizeModelId(value)
    if (!id) continue
    if (dropWildcards && id.includes('*')) continue
    if (!ids.includes(id)) ids.push(id)
  }
  return ids
}

/**
 * First text-capable model; '' when the shelf has no text model. A
 * media-only shelf must yield no default: single-model text configs would
 * reference a media id that the generated config entries intentionally
 * exclude (e.g. Grok `[model.*]` entries filter media ids out).
 */
export function pickPrimaryModel(models: readonly string[]): string {
  return models.find((id) => !isMediaModelId(id)) ?? ''
}

/** Escape a value embedded in a double-quoted Bourne-shell string. */
export function escapeShDq(value: string): string {
  return value
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\$/g, '\\$')
    .replace(/`/g, '\\`')
}

/**
 * Escape a value embedded in a double-quoted PowerShell string.
 *
 * Besides the ASCII delimiter, PowerShell's tokenizer also treats the
 * Unicode double quotes U+201C / U+201D and the low-9 quote U+201E as
 * double-quoted-string boundaries (verified by scanning every Unicode
 * punctuation/symbol code point against the PowerShell parser — only
 * U+0022, U+201C, U+201D and U+201E act as boundaries). An unescaped value
 * containing one of them can terminate the string early and inject
 * commands, e.g. a model id `audit\u201d; Write-Output PWNED; #`. A backtick
 * prefix neutralizes all of them: U+201C/U+201D normalize to ASCII quotes
 * and U+201E is preserved as a literal.
 */
export function escapePsDq(value: string): string {
  return value
    .replace(/`/g, '``')
    .replace(/"/g, '`"')
    .replace(/\$/g, '`$')
    .replace(/\u201c/g, '`\u201c')
    .replace(/\u201d/g, '`\u201d')
    .replace(/\u201e/g, '`\u201e')
}

/** Escape a value embedded in an unquoted Windows CMD `set` assignment. */
export function escapeCmdValue(value: string): string {
  return value
    .replace(/%/g, '%%')
    .replace(/\^/g, '^^')
    .replace(/&/g, '^&')
    .replace(/\|/g, '^|')
    .replace(/</g, '^<')
    .replace(/>/g, '^>')
}

/** Escape a TOML basic string (double-quoted), stripping illegal control chars. */
export function escapeTomlBasicString(value: string): string {
  const backspace = fromCodes(8)
  const tab = fromCodes(9)
  const lf = fromCodes(10)
  const ff = fromCodes(12)
  const cr = fromCodes(13)
  return stripChars(value, TOML_STRIP_CHARS)
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .split(backspace).join('\\b')
    .split(tab).join('\\t')
    .split(lf).join('\\n')
    .split(ff).join('\\f')
    .split(cr).join('\\r')
}
