import { Component, type ErrorInfo, type ReactNode } from 'react';

// ErrorFallback replaces a part of the app that failed to render. React has already
// removed that part, so its unsaved changes are gone: say so plainly. Reload happens
// only when the person chooses it.
export function ErrorFallback({ onReload }: { onReload: () => void }) {
  return (
    <section className="panel empty" role="alert">
      <h2>Something went wrong</h2>
      <p>This page stopped working. Unsaved changes on this page could not be kept.</p>
      <p>Reload the app to try again. Saved patterns, calendars and house layouts are safe.</p>
      <button className="button primary" onClick={onReload}>
        Reload the app
      </button>
    </section>
  );
}

// ErrorBoundary catches a render error below it and shows ErrorFallback instead of
// a blank screen. Give it a key (for example the page) to start again on navigation.
export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() {
    return { failed: true };
  }
  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.error('Trimlights render error', error, info.componentStack);
  }
  render() {
    return this.state.failed ? (
      <ErrorFallback onReload={() => location.reload()} />
    ) : (
      this.props.children
    );
  }
}
