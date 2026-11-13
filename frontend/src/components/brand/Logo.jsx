import logoDark from "assets/brand/logo-dark.svg";
import logoLight from "assets/brand/logo-light.svg";
import { useDark } from "lib/useDark";
import { cn } from "lib/utils";

// NullOps logo. "dark" is the variant for dark backgrounds (white strokes),
// "light" the one for light backgrounds (black strokes): picked by the theme.
export default function Logo({ className, alt = "NullOps" }) {
  const dark = useDark();
  return <img src={dark ? logoDark : logoLight} alt={alt} className={cn("w-auto select-none", className)} draggable={false} />;
}
