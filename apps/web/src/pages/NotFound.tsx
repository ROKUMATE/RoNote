import { Link } from "@tanstack/react-router";
import { PageTitle } from "../ui/PageTitle";

export function NotFound() {
  return (
    <div className="max-w-[68ch]">
      <PageTitle>Page not found</PageTitle>
      <p className="mt-3 text-ui-lg">
        <Link to="/" className="text-accent underline underline-offset-2">
          Go to the library
        </Link>
      </p>
    </div>
  );
}
