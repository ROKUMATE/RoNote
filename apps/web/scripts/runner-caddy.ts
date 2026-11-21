import { caddyRunnerSite } from "../runner-headers.ts";

/*
 * Prints Caddy's runner site, which make generate writes to
 * deploy/caddy/runner.caddy.
 *
 *   node scripts/runner-caddy.ts
 */
process.stdout.write(caddyRunnerSite());
