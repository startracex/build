import { add } from "./math.js";
import greet from "./greet.js";
import { label } from "./reexport.js";
import * as math from "./math.js";
import { n, inc } from "./counter.js";
import { banner } from "./banner.js";
import { Hello } from "./ui.js";
import { note } from "./notes";
import type { Ghost } from "./missing-types.js";

export { add, greet, label, banner, Hello, note };
export const product = math.mul(2, 4);
export default greet;

export function read(): number {
  inc();
  return n;
}

export function load(): Promise<number> {
  return import("./dep.js").then((mod) => mod.value);
}

export type ID = Ghost;
