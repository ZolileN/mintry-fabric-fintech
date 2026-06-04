import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'Mintry Fabric Dashboard',
  description: 'FinOps policy and telemetry console for Mintry Fabric',
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  )
}
