import { fromMain } from "pkg-main";
import { fromModule } from "pkg-module";
export { fromExports } from "pkg-exports";
import { fromSub } from "pkg-exports/sub";
import meta from "./meta.json";

export { fromMain, fromModule,  fromSub };
export const title = meta.title;
