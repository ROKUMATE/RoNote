/** Where make web-e2e runs each part of the stack. */
export const stack = {
  web: "http://127.0.0.1:5180",
  api: "http://127.0.0.1:18080",
  // A name, not an IP: the API refuses IP-literal hosts.
  fixtures: "http://localhost:18090",
} as const;
