import type { ReactNode } from "react";

export function PageTitle({ children }: { children: ReactNode }) {
  return <h1 className="font-semibold text-h1 leading-tight">{children}</h1>;
}
