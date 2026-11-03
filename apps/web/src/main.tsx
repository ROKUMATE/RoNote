import "@fontsource-variable/source-serif-4/wght.css";
import "@fontsource-variable/source-serif-4/wght-italic.css";
import "@fontsource-variable/ibm-plex-sans/wght.css";
import "@fontsource-variable/ibm-plex-sans/wght-italic.css";
import "@fontsource-variable/jetbrains-mono/wght.css";
import "./styles/app.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { createQueryClient } from "./api/queries";
import { ImportTracker } from "./imports/tracker";
import { createAppRouter } from "./router";

const root = document.getElementById("root");
if (!root) {
  throw new Error("index.html has no #root element");
}
const queryClient = createQueryClient();
createRoot(root).render(
  <StrictMode>
    <App
      router={createAppRouter()}
      queryClient={queryClient}
      tracker={new ImportTracker(queryClient)}
    />
  </StrictMode>,
);
