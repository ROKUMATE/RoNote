/*
 * JavaScript as the classifier reads it (classify.ts): code with its
 * comments gone and the text of its strings, templates and regular
 * expressions blanked, so a word in a string never counts as code; and the
 * same without its comments alone, for the modules it names.
 */

const pairs: Record<string, string> = { "(": ")", "[": "]", "{": "}" };

/** What a slash follows when it starts a regular expression rather than dividing. */
const beforeRegex =
  /(?:[(,=:[!&|?{};+\-*%<>~^]|\b(?:return|typeof|instanceof|in|of|new|delete|void|throw|case|do|else|yield|await))$/;

export type JsCode = {
  /** The code with comments gone and literals' text blanked. */
  code: string;
  /** The source with comments gone, literals kept. */
  uncommented: string;
  /** Whether a bracket, string, template, regular expression or comment is left open, or a bracket closed that never opened. */
  open: boolean;
};

export function jsCode(source: string): JsCode {
  let code = "";
  // Open brackets, and "${" for each template expression the code is inside.
  const stack: string[] = [];
  let mode: "code" | "line" | "block" | "'" | '"' | "`" | "regex" = "code";
  let inClass = false;
  const comments: [number, number][] = [];
  let commentStart = 0;

  const read = (open: boolean): JsCode => {
    if (mode === "line" || mode === "block") comments.push([commentStart, source.length]);
    let uncommented = "";
    let from = 0;
    for (const [start, end] of comments) {
      uncommented += source.slice(from, start) + source.slice(start, end).replace(/[^\n]/g, " ");
      from = end;
    }
    return { code, uncommented: uncommented + source.slice(from), open };
  };

  for (let i = 0; i < source.length; i++) {
    const c = source[i] ?? "";
    const next = source[i + 1];
    switch (mode) {
      case "code":
        if (c === "/" && (next === "/" || next === "*")) {
          mode = next === "/" ? "line" : "block";
          commentStart = i;
          i++;
        } else if (c === "/" && (code.trimEnd() === "" || beforeRegex.test(code.trimEnd()))) {
          mode = "regex";
          code += c;
        } else if (c === "'" || c === '"' || c === "`") {
          mode = c;
          code += c;
        } else if (c in pairs) {
          stack.push(c);
          code += c;
        } else if (c === ")" || c === "]" || c === "}") {
          const top = stack.pop();
          code += c;
          if (top === "${" && c === "}") mode = "`";
          else if (top === undefined || pairs[top] !== c) return read(true);
        } else {
          code += c;
        }
        break;
      case "line":
        if (c === "\n") {
          comments.push([commentStart, i]);
          mode = "code";
          code += c;
        }
        break;
      case "block":
        if (c === "*" && next === "/") {
          comments.push([commentStart, i + 2]);
          mode = "code";
          code += " ";
          i++;
        } else if (c === "\n") {
          code += c;
        }
        break;
      case "'":
      case '"':
        if (c === "\\") i++;
        else if (c === mode) {
          mode = "code";
          code += c;
        } else if (c === "\n") return read(true);
        break;
      case "`":
        if (c === "\\") i++;
        else if (c === "`") {
          mode = "code";
          code += c;
        } else if (c === "$" && next === "{") {
          stack.push("${");
          mode = "code";
          code += "${";
          i++;
        } else if (c === "\n") code += c;
        break;
      case "regex":
        if (c === "\\") i++;
        else if (c === "[") inClass = true;
        else if (c === "]") inClass = false;
        else if (c === "/" && !inClass) {
          mode = "code";
          code += c;
        } else if (c === "\n") return read(true);
        break;
    }
  }
  return read((mode !== "code" && mode !== "line") || stack.length > 0);
}
