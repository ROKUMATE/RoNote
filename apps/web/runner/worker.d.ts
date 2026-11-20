declare module "*.py?raw" {
  const source: string;
  export default source;
}

interface ImportMeta {
  readonly env: {
    /** Where Pyodide's files are served, with its version (vite.runner.config.ts). */
    readonly RONOTE_PYODIDE_URL: string;
  };
}
