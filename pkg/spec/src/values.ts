// Author-facing, fluent, seeded value generators. Each builder is itself a
// Sampler<T> (it has .generate()), so `integers().between(0, 9)` is usable
// anywhere a Sampler is expected and chains read left to right.
//
// Every draw goes through the shared samplerRng (sampler-rng.ts) that from()
// uses, so generators are deterministic AND identical across the goja verifier
// and the V8 web runtime. Outside an actions() walk samplerRng is null and each
// generator returns a fixed deterministic default (mirroring from()'s index 0),
// so module-load-time calls never throw and never use an unseeded source.
//
// The model policy is the one caller that must not take that default: it walks
// the authored leaves on every step, outside the rng, so the default would be
// the value it types forever while the seeded arm varies it. A generator that
// spans more than one value refuses there instead.

import type { Pcg } from "./pcg.ts";
import type { Sampler } from "./types.ts";
import { getSamplerRng, refuseValueWhileEnumerating } from "./sampler-rng.ts";
import { INPUT_CORPUS } from "./corpus.ts";

const ALPHA = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ";
const DIGITS = "0123456789";
const ALPHANUMERIC = ALPHA + DIGITS;

function draw(rng: Pcg | null, bound: number): number {
  return rng && bound > 1 ? rng.intN(bound) : 0;
}

class StringBuilder implements Sampler<string> {
  private minLength = 1;
  private maxLength = 16;
  private charset = ALPHANUMERIC;

  length(min: number, max: number): this {
    this.minLength = min;
    this.maxLength = max;
    return this;
  }

  alpha(): this {
    this.charset = ALPHA;
    return this;
  }

  // A max below the min never reaches the draw, so the count is minLength, and
  // a count below zero emits nothing: length(-2, 0) is the empty string either
  // way, and both policies agree on it.
  private spansMoreThanOneString(): boolean {
    const shortest = Math.max(this.minLength, 0);
    const longest = Math.max(this.maxLength, this.minLength, 0);
    return longest > shortest || (longest > 0 && this.charset.length > 1);
  }

  generate(): string {
    if (this.spansMoreThanOneString()) refuseValueWhileEnumerating("strings()");
    const rng = getSamplerRng();
    const span = this.maxLength - this.minLength + 1;
    const count = this.minLength + draw(rng, span);
    let result = "";
    for (let i = 0; i < count; i++) {
      result += this.charset[draw(rng, this.charset.length)];
    }
    return result;
  }
}

class IntegerBuilder implements Sampler<number> {
  private minValue = 0;
  private maxValue = 2 ** 31 - 1;

  between(min: number, max: number): this {
    this.minValue = min;
    this.maxValue = max;
    return this;
  }

  generate(): number {
    const rng = getSamplerRng();
    const span = this.maxValue - this.minValue + 1;
    if (span > 1) refuseValueWhileEnumerating("integers()");
    return this.minValue + draw(rng, span);
  }
}

class EmailBuilder implements Sampler<string> {
  private host = "example.com";

  domain(domain: string): this {
    this.host = domain;
    return this;
  }

  // The local part always spans 3 to 8 alphabetic characters and only the host
  // is author-set, so an address is never a single value.
  generate(): string {
    refuseValueWhileEnumerating("emails()");
    const local = new StringBuilder().length(3, 8).alpha().generate();
    return `${local}@${this.host}`;
  }
}

class EdgeCaseTextBuilder implements Sampler<string> {
  generate(): string {
    if (INPUT_CORPUS.length > 1) refuseValueWhileEnumerating("edgeCaseText()");
    const rng = getSamplerRng();
    return INPUT_CORPUS[draw(rng, INPUT_CORPUS.length)] ?? "";
  }
}

export function strings(): StringBuilder {
  return new StringBuilder();
}

export function integers(): IntegerBuilder {
  return new IntegerBuilder();
}

export function emails(): EmailBuilder {
  return new EmailBuilder();
}

export function edgeCaseText(): EdgeCaseTextBuilder {
  return new EdgeCaseTextBuilder();
}
