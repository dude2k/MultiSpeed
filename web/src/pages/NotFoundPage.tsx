import { ArrowLeft, Compass } from 'lucide-react'
import { Link } from 'react-router'
import { Button } from '../components/ui/button'
import { Card } from '../components/ui/card'

export default function NotFoundPage() {
  return <Card className="mx-auto mt-12 max-w-lg p-8 text-center"><span className="mx-auto grid h-12 w-12 place-items-center rounded-xl border border-border bg-muted text-muted-foreground"><Compass className="h-5 w-5" /></span><p className="mt-5 text-[11px] font-bold uppercase tracking-[.15em] text-primary">404 · Unknown route</p><h2 className="mt-2 text-2xl font-bold tracking-tight">This path is not monitored.</h2><p className="mt-2 text-sm leading-6 text-muted-foreground">The requested console page does not exist or was moved.</p><Button asChild className="mt-6"><Link to="/"><ArrowLeft className="h-4 w-4" />Return to dashboard</Link></Button></Card>
}
