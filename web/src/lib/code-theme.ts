import { createCssVariablesTheme } from "shiki/core";

// One code theme for the site: token colours are CSS variables set in app/global.css, so code
// follows the light/dark switch without a second render.
export const codeTheme = createCssVariablesTheme({
  name: "baton",
  variablePrefix: "--code-",
  variableDefaults: {},
  fontStyle: true,
});
