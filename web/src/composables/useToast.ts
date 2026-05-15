import { ref } from 'vue'

export interface Toast {
  id: number
  type: 'success' | 'error' | 'info'
  message: string
  persistent: boolean
}

const toasts = ref<Toast[]>([])
let nextId = 1

export function useToast() {
  function add(type: Toast['type'], message: string, duration = 3500) {
    const id = nextId++
    const persistent = duration <= 0
    toasts.value.push({ id, type, message, persistent })
    if (!persistent) setTimeout(() => remove(id), duration)
  }

  function remove(id: number) {
    const idx = toasts.value.findIndex((t) => t.id === id)
    if (idx !== -1) toasts.value.splice(idx, 1)
  }

  return {
    toasts,
    success: (msg: string) => add('success', msg),
    error: (msg: string) => add('error', msg, 0),
    info: (msg: string) => add('info', msg),
    add,
    remove,
  }
}
