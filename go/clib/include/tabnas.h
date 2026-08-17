/* tabnas.h — the uniform C ABI of the per-format tabnas clibs (ADR-12).
 *
 * tabnas-clib-template: v1
 *
 * Every per-format library (libtabnasjson, libtabnastoml, …) exports
 * exactly these five symbols; the format is fixed at build time by
 * which library you load. One consequence is deliberate: two format
 * clibs cannot be statically linked into one binary — load them
 * dynamically (dlopen / LoadLibrary / your language's FFI loader).
 *
 * CONTRACT
 *   - Every function returning char* returns a malloc'd JSON document;
 *     release it with tabnas_free (it is free(3) — do not use another
 *     allocator's free).
 *   - Three outcomes: a broken call is {"ok":false,"error":{"code":…}};
 *     input outside the language is {"ok":true,"accept":false,
 *     "error":{…}}; accepted input is {"ok":true,"accept":true} plus
 *     "value" where the format's parse result is JSON-representable.
 *   - Lengths are explicit byte counts; buffers are NOT read as
 *     NUL-terminated strings. (NULL, 0) is the empty buffer.
 *   - Handles are safe to use from several threads; each carries a
 *     mutex inside the library.
 *
 * This header is consumed directly by C, C++, Zig (@cImport), Swift
 * (module map), Nim, D — no per-language binding layer exists or is
 * needed for them.
 */
#ifndef TABNAS_CLIB_H
#define TABNAS_CLIB_H

#ifdef __cplusplus
extern "C" {
#endif

/* {"ok":true,"lib":"…","format":"…","template":"…"} */
char *tabnas_version(void);

/* Build a parser; returns {"ok":true,"handle":N} or a failure document.
 * opts_json is an options JSON document — RESERVED; pass (NULL, 0). */
char *tabnas_grammar(const char *opts_json, int opts_len);

/* Parse src against the library's format. */
char *tabnas_parse(long long handle, const char *src, int src_len);

/* Release a handle. Unknown or already-freed handles are a no-op. */
void tabnas_grammar_free(long long handle);

/* Release any char* returned by this library. NULL is a no-op. */
void tabnas_free(char *doc);

#ifdef __cplusplus
}
#endif

#endif /* TABNAS_CLIB_H */
