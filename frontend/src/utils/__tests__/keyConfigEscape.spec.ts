import { describe, expect, it } from 'vitest'
import {
  escapeCmdValue,
  escapePsDq,
  escapeShDq,
  escapeTomlBasicString,
  isMediaModelId,
  normalizeModelIds,
  pickPrimaryModel,
  sanitizeModelId
} from '../keyConfigEscape'

describe('sanitizeModelId', () => {
  it('strips control characters and trims whitespace', () => {
    expect(sanitizeModelId('  claude-sonnet-4-5 \n')).toBe('claude-sonnet-4-5')
    expect(sanitizeModelId('claude\u0000sonnet')).toBe('claudesonnet')
    expect(sanitizeModelId('a\rb\nc')).toBe('abc')
  })

  it('returns empty string for non-strings and empty values', () => {
    expect(sanitizeModelId(undefined)).toBe('')
    expect(sanitizeModelId(null)).toBe('')
    expect(sanitizeModelId(42)).toBe('')
    expect(sanitizeModelId('   ')).toBe('')
  })
})

describe('normalizeModelIds', () => {
  it('dedupes sanitized ids preserving order and drops wildcards when asked', () => {
    expect(normalizeModelIds(['a', 'a', 'b'])).toEqual(['a', 'b'])
    expect(normalizeModelIds(['*', 'a', '*'], { dropWildcards: true })).toEqual(['a'])
    expect(normalizeModelIds(['*', 'a'])).toEqual(['*', 'a'])
  })

  it('returns [] for non-arrays', () => {
    expect(normalizeModelIds(undefined)).toEqual([])
    expect(normalizeModelIds('nope')).toEqual([])
  })
})

describe('isMediaModelId / pickPrimaryModel', () => {
  it('classifies media ids', () => {
    expect(isMediaModelId('grok-imagine-image')).toBe(true)
    expect(isMediaModelId('veo-3-fast')).toBe(true)
    expect(isMediaModelId('claude-sonnet-4-5')).toBe(false)
  })

  it('prefers the first text-capable model', () => {
    expect(pickPrimaryModel(['grok-imagine-image', 'grok-4.5'])).toBe('grok-4.5')
    expect(pickPrimaryModel([])).toBe('')
  })

  it('never picks a media-only id as the text default', () => {
    // A media-only shelf must yield no default instead of a media id that
    // single-model text configs would reference without a matching entry.
    expect(pickPrimaryModel(['grok-imagine-image', 'veo-3-fast'])).toBe('')
  })
})

describe('escapeShDq', () => {
  it('escapes backslash, quote, dollar and backtick', () => {
    expect(escapeShDq('a"b$c`d\\e')).toBe('a\\"b\\$c\\`d\\\\e')
  })
})

describe('escapePsDq', () => {
  it('escapes backtick, ASCII double quote and dollar sign', () => {
    expect(escapePsDq('a"b$c`d')).toBe('a`"b`$c``d')
  })

  // PowerShell tokenizer boundary characters: verified by scanning every
  // Unicode punctuation/symbol code point against the PowerShell parser —
  // only U+0022, U+201C, U+201D and U+201E act as double-quoted-string
  // boundaries. The simulator below encodes exactly that rule set so any
  // regression in the escape list fails the test.
  const PS_BOUNDARY_CHARS = ['"', '\u201c', '\u201d', '\u201e']

  /**
   * Scan escaped PowerShell double-quoted-string content the way the
   * tokenizer treats boundary characters: a backtick escapes the next
   * character; any unescaped boundary character closes the string. Returns
   * the offset of an early close (command injection), `'unterminated'` when
   * a trailing backtick would swallow the template's closing quote, or -1
   * when the content is safe.
   */
  function psDqBreakpoint(content: string): number | 'unterminated' {
    let i = 0
    while (i < content.length) {
      const ch = content[i]
      if (ch === '`') {
        if (i === content.length - 1) return 'unterminated'
        i += 2
        continue
      }
      if (PS_BOUNDARY_CHARS.includes(ch)) return i
      i += 1
    }
    return -1
  }

  it('never lets a hostile model id close the surrounding double-quoted string', () => {
    const adversarialIds = [
      // Audit P1 payload (PR #163): Unicode right double quote breakout.
      'audit\u201d; Write-Output PR163_INJECTION_CONFIRMED; #',
      // Every PowerShell string-boundary character, in every position.
      '\u201c',
      '\u201d',
      '\u201e',
      '"',
      'audit\u201c; Write-Output PWNED; #',
      'audit\u201e; Write-Output PWNED; #',
      'a\u201db\u201dc',
      // Interactions with the other escapes.
      '`\u201d',
      '\u201d`',
      '$\u201d',
      'a\u201d${env:PASSWORD}',
      '``` \u201d ```'
    ]
    for (const id of adversarialIds) {
      const line = `$env:MODEL="${escapePsDq(id)}"`
      const open = line.indexOf('"')
      const content = line.slice(open + 1, -1)
      // The escaped value must not terminate the string early (command
      // injection) or leave it unterminated.
      expect(psDqBreakpoint(content), `id=${JSON.stringify(id)} line=${line}`).toBe(-1)
    }
  })

  it('prefixes boundary characters with a backtick and leaves the rest intact', () => {
    // The backtick prefix neutralizes the boundary semantics; when PowerShell
    // evaluates the escaped string, U+201C/U+201D normalize to ASCII quotes
    // and U+201E is preserved (verified runtime behavior).
    expect(escapePsDq('a\u201db')).toBe('a`\u201db')
    expect(escapePsDq('a\u201cb')).toBe('a`\u201cb')
    expect(escapePsDq('a\u201eb')).toBe('a`\u201eb')
    expect(escapePsDq('plain-model-1')).toBe('plain-model-1')
  })
})

describe('escapeCmdValue', () => {
  it('escapes percent and metacharacters for unquoted CMD set values', () => {
    expect(escapeCmdValue('a%b^c&d|e<f>g')).toBe('a%%b^^c^&d^|e^<f^>g')
  })
})

describe('escapeTomlBasicString', () => {
  it('escapes backslash and quote, encodes control chars', () => {
    expect(escapeTomlBasicString('a"b\\c')).toBe('a\\"b\\\\c')
    expect(escapeTomlBasicString('a\nb')).toBe('a\\nb')
  })

  it('strips illegal control characters', () => {
    expect(escapeTomlBasicString('a\u0000b')).toBe('ab')
  })
})
