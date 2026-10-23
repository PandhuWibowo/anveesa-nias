import { ref } from 'vue'
import { useAuth } from '@/composables/useAuth'
import { NAV_SECTIONS, navKey, type NavItem, type NavSection } from '@/navigation'

const LAST_KEY = 'nias:lastInSection'

// Last page visited per section, so clicking a section returns you to where you were.
const lastInSection = ref<Record<string, string>>(readLast())

function readLast(): Record<string, string> {
  try {
    const v = JSON.parse(localStorage.getItem(LAST_KEY) || '{}')
    return v && typeof v === 'object' ? v : {}
  } catch {
    return {}
  }
}

export function useNavigation() {
  const { hasAnyPermission } = useAuth()

  function permitted(item: NavItem) {
    return !item.permissionsAny?.length || hasAnyPermission(item.permissionsAny)
  }

  /**
   * Pages a section offers right now. Connection-scoped pages only appear when the
   * active connection uses a matching driver; the page you are on always stays listed.
   */
  function visibleItems(section: NavSection, activeDriver: string | null | undefined, current?: { name: string; tab?: string | null }) {
    return section.items.filter((item) => {
      if (!permitted(item)) return false
      if (current && item.name === current.name && (!item.query?.tab || item.query.tab === current.tab)) return true
      if (!section.connectionScoped || !item.drivers) return true
      return !!activeDriver && item.drivers.includes(activeDriver)
    })
  }

  function rememberVisit(section: NavSection, item: NavItem) {
    lastInSection.value = { ...lastInSection.value, [section.id]: navKey(item) }
    try {
      localStorage.setItem(LAST_KEY, JSON.stringify(lastInSection.value))
    } catch {
      // storage unavailable: sections just open on their first page
    }
  }

  /** Where clicking a section should go: the last page used there, else its first page. */
  function entryFor(section: NavSection, activeDriver: string | null | undefined): NavItem | undefined {
    const items = visibleItems(section, activeDriver)
    const last = lastInSection.value[section.id]
    return items.find((i) => navKey(i) === last) ?? items[0]
  }

  return { permitted, visibleItems, rememberVisit, entryFor, sections: NAV_SECTIONS }
}
