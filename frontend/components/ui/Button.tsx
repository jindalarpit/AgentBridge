// Button — base UI button component
// Implementation as needed by feature tasks
import { ButtonHTMLAttributes } from "react";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "ghost";
}

export function Button({ variant = "primary", className = "", ...props }: ButtonProps) {
  return <button className={`rounded px-4 py-2 ${className}`} {...props} />;
}
