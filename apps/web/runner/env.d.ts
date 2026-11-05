/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** The origins of the app that may frame and talk to the runner (vite.runner.config.ts). */
  readonly RONOTE_APP_ORIGINS: readonly string[];
}
