import { fromMain } from "pkg-main";
import { fromModule } from "pkg-module";
import { fromExports } from "pkg-exports";
import { fromSub } from "pkg-exports/sub";
import meta from "./meta.json";

export { fromMain, fromModule, fromExports, fromSub };
export const title = meta.title;
