import type { Plugin } from "vite";

/*
 * index.html names the deployment's origins as Caddy template calls,
 * {{env "NAME"}}, which Caddy fills from its environment in production
 * (deploy/caddy/Caddyfile). The dev servers fill them the same way, from
 * the same names; a build keeps them for Caddy.
 */

const placeholder = /\{\{env "(RONOTE_[A-Z_]+)"\}\}/g;

/** Fills index.html's {{env "NAME"}} with value(NAME). */
export function fill(html: string, value: (name: string) => string): string {
  return html.replace(placeholder, (_, name: string) => value(name));
}

/** Fills index.html's {{env "NAME"}} with process.env, or with defaults. */
export function fillOrigins(html: string, defaults: Record<string, string>): string {
  return fill(html, (name) => process.env[name] ?? defaults[name] ?? "");
}

/** Fills index.html's origins on the dev server. */
export function origins(defaults: Record<string, string>): Plugin {
  return {
    name: "ronote-origins",
    apply: "serve",
    transformIndexHtml: (html) => fillOrigins(html, defaults),
  };
}
