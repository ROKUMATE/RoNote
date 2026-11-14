/** Where make web-e2e runs each part of the stack. */
export const stack = {
  web: "http://127.0.0.1:5180",
  api: "http://127.0.0.1:18080",
  // A second API whose embedder is down, for degraded search.
  apiDegraded: "http://127.0.0.1:18082",
  // A third API that allows one chat question a day.
  apiOneQuestion: "http://127.0.0.1:18083",
  // localhost, not the web app's 127.0.0.1: cookies ignore ports.
  runner: "http://localhost:5181",
  // A name, not an IP: the API refuses IP-literal hosts.
  fixtures: "http://localhost:18090",
  // The fake GitHub sign-in (cmd/fakeidp).
  idp: "http://127.0.0.1:18091",
  // The fake embedder (cmd/fakeembed).
  embed: "http://127.0.0.1:18093",
  // The fake chat model (cmd/fakellm): no test reaches a real one.
  llm: "http://127.0.0.1:18094",
} as const;
