import { parse } from "yaml";
import specText from "../../../../api/openapi.yaml?raw";

interface SchemaNode {
  enum?: unknown[];
  maxLength?: number;
  properties?: Record<string, SchemaNode>;
  items?: SchemaNode;
  allOf?: SchemaNode[];
}

/** The schemas of api/openapi.yaml, read the way the generator reads them. */
export const specSchemas: Record<string, SchemaNode> = parse(specText).components.schemas;

/** The values of the named string enum in the spec. */
export function specEnum(name: string): string[] {
  const values = specSchemas[name]?.enum;
  if (!values) throw new Error(`api/openapi.yaml has no enum schema ${name}`);
  return values.map(String);
}

/** Every string enum in the spec, top-level or nested in a schema, by where it is. */
export function allSpecEnums(): Map<string, string[]> {
  const found = new Map<string, string[]>();
  const visit = (where: string, node: SchemaNode | undefined) => {
    if (!node) return;
    const strings = node.enum?.filter((v): v is string => typeof v === "string") ?? [];
    if (strings.length > 1) found.set(where, strings);
    for (const [key, child] of Object.entries(node.properties ?? {}))
      visit(`${where}.${key}`, child);
    visit(`${where}[]`, node.items);
    for (const [i, part] of (node.allOf ?? []).entries()) visit(`${where}.allOf${i}`, part);
  };
  for (const [name, schema] of Object.entries(specSchemas)) visit(name, schema);
  return found;
}

/** The snake_case property names of every object schema in the spec. */
export function specSnakeProperties(): Map<string, string[]> {
  const found = new Map<string, string[]>();
  const visit = (where: string, node: SchemaNode | undefined) => {
    if (!node) return;
    const names = Object.keys(node.properties ?? {}).filter((key) => key.includes("_"));
    if (names.length > 0) found.set(where, names);
    for (const [key, child] of Object.entries(node.properties ?? {}))
      visit(`${where}.${key}`, child);
    visit(`${where}[]`, node.items);
    for (const [i, part] of (node.allOf ?? []).entries()) visit(`${where}.allOf${i}`, part);
  };
  for (const [name, schema] of Object.entries(specSchemas)) visit(name, schema);
  return found;
}
