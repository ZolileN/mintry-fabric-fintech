export default function HomePage() {
  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 p-8">
      <div className="mx-auto max-w-6xl space-y-8">
        <section className="rounded-3xl border border-slate-800 bg-slate-900/90 p-10 shadow-2xl shadow-slate-950/30 backdrop-blur-xl">
          <p className="text-sm uppercase tracking-[0.3em] text-emerald-300">Mintry Fabric</p>
          <h1 className="mt-4 text-5xl font-semibold leading-tight text-white">Fintech Cost & Security Proxy</h1>
          <p className="mt-6 max-w-3xl text-lg leading-8 text-slate-300">
            Invisible sidecar runtime for duplicate vendor request elimination, encrypted local cache, and policy-driven TTL controls.
          </p>
        </section>

        <section className="grid gap-6 lg:grid-cols-3">
          {cards.map((card) => (
            <article key={card.title} className="rounded-3xl border border-slate-800 bg-slate-900/95 p-6 shadow-xl shadow-slate-950/20">
              <h2 className="text-2xl font-semibold text-white">{card.title}</h2>
              <p className="mt-4 text-slate-300">{card.description}</p>
            </article>
          ))}
        </section>
      </div>
    </main>
  )
}

const cards = [
  {
    title: 'Policy Manager',
    description: 'Create and tune route-level TTL and cache error rules for verification vendors.',
  },
  {
    title: 'Telemetry Overview',
    description: 'Monitor cache hit ratio, outbound request count, and fail-open events in real time.',
  },
  {
    title: 'Security Posture',
    description: 'Validate local storage encryption, data isolation, and compliance guardrail status.',
  },
]
