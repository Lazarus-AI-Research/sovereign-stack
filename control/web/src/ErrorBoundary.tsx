import { Component, ErrorInfo, ReactNode } from "react";

export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Sovereign portal rendering error", error, info.componentStack);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return <main className="page-error" role="alert">
      <section className="card">
        <span className="eyebrow">Portal error</span>
        <h1>This page could not be displayed</h1>
        <p>Your appliance data is safe. Reload the page to retry; if the problem continues, open System status and download diagnostics.</p>
        <div className="actions"><button onClick={() => window.location.reload()}>Reload portal</button><a className="button secondary" href="/admin/system">System status</a></div>
      </section>
    </main>;
  }
}
