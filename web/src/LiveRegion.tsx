import type { ReactNode } from 'react';

// Screen readers announce changes inside a live region that already exists, but
// often miss a region that is inserted together with its text. Keep this
// region mounted and render the message inside it only when there is one.
export function LiveStatus({ children, className }: { children?: ReactNode; className?: string }) {
  return (
    <div role="status" className={className}>
      {children}
    </div>
  );
}
