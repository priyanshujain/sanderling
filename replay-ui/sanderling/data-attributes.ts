// attrs is keyed by the names the markup writes (rawAttributes in
// pkg/spec/src/web-runtime.ts), and the goja host the properties run in exposes
// attrs and nothing else (internal/verifier/marshal.go): there is no dataset
// field to read a camelCased key off, so the spec converts the key itself.
export function attributeOf(key: string): string {
  return `data-${key.replace(/[A-Z]/g, (letter) => `-${letter.toLowerCase()}`)}`;
}
