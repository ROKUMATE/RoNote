import { buildPages } from "../runner-pages.ts";

/*
 * Makes the runner's Cloudflare Pages site from its build, for the app at
 * APP_URL; the release deploys TO with wrangler.
 *
 *   node scripts/runner-pages.ts APP_URL [FROM] [TO]   FROM dist-runner, TO dist-pages
 */
const [appURL, from = "dist-runner", to = "dist-pages"] = process.argv.slice(2);
if (!appURL) {
  console.error("usage: node scripts/runner-pages.ts APP_URL [FROM] [TO]");
  process.exit(2);
}
buildPages(from, to, appURL);
console.log(`runner-pages: ${to} for ${new URL(appURL).origin}`);
