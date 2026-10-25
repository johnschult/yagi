import { Info } from 'lucide-react'
import type { ReactNode } from 'react'
import { Drawer, DrawerContent, DrawerDescription, DrawerTitle, DrawerTrigger } from '@/components/ui/drawer'
import { Popover, PopoverContent, PopoverDescription, PopoverTitle, PopoverTrigger } from '@/components/ui/popover'
import { useIsPhone } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'

const BUTTON =
  'inline-flex size-5 shrink-0 items-center justify-center rounded-full text-muted-foreground outline-none hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 data-popup-open:text-foreground'

/**
 * A small (i) that explains a setting, a card or a heading: a popover beside it, or on a phone a sheet from the
 * bottom. It keeps the explanation out of the page until it is wanted. `about` names what is explained ("Guide"), for
 * the sheet's title and for screen readers.
 */
export function InfoTip({ about, children, className }: { about: string; children: ReactNode; className?: string }) {
  const phone = useIsPhone()
  const label = `About ${about}`
  const mark = <Info aria-hidden className="size-3.5" />
  if (phone) {
    return (
      <Drawer>
        <DrawerTrigger aria-label={label} className={cn(BUTTON, className)}>
          {mark}
        </DrawerTrigger>
        <DrawerContent>
          <div className="space-y-2 px-5 pt-8 pb-6">
            <DrawerTitle>{about}</DrawerTitle>
            <DrawerDescription asChild>
              <div className="space-y-2 text-sm leading-relaxed">{children}</div>
            </DrawerDescription>
          </div>
        </DrawerContent>
      </Drawer>
    )
  }
  return (
    <Popover>
      <PopoverTrigger aria-label={label} className={cn(BUTTON, className)}>
        {mark}
      </PopoverTrigger>
      <PopoverContent align="start" className="p-3.5">
        {/* The (i) sits beside what it explains, so the title is only for screen readers. */}
        <PopoverTitle className="sr-only">{about}</PopoverTitle>
        <PopoverDescription render={<div />} className="space-y-2 text-xs leading-relaxed">
          {children}
        </PopoverDescription>
      </PopoverContent>
    </Popover>
  )
}
