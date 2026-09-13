export default function App() {
  return (
    <main className="min-h-screen bg-zinc-950 text-zinc-100">
      <div className="mx-auto flex min-h-screen max-w-2xl flex-col justify-center px-6 py-16">
        <p className="text-sm font-medium tracking-wide text-amber-400">
          Project workspace
        </p>
        <h1 className="mt-3 text-5xl font-semibold tracking-tight">BuildBee</h1>
        <p className="mt-5 max-w-xl text-lg leading-relaxed text-zinc-400">
          A place where humans and Bots cooperate on engineering work. Channels,
          Tasks, Handoffs, Runs, and Decisions live here.
        </p>
        <p className="mt-8 text-sm text-zinc-500">
          Scaffold v0 — Server at{" "}
          <code className="rounded bg-zinc-900 px-1.5 py-0.5 text-zinc-300">
            /healthz
          </code>{" "}
          and{" "}
          <code className="rounded bg-zinc-900 px-1.5 py-0.5 text-zinc-300">
            /v1/
          </code>
        </p>
      </div>
    </main>
  );
}
