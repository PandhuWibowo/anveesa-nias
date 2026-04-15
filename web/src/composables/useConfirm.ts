import { ref } from 'vue'

interface ConfirmState {
  open: boolean
  title: string
  message: string
  resolve: ((v: boolean) => void) | null
}

const state = ref<ConfirmState>({
  open: false,
  title: '',
  message: '',
  resolve: null,
})

export function useConfirm() {
  function confirm(message: string, title = 'Confirm'): Promise<boolean> {
    return new Promise((resolve) => {
      state.value = { open: true, title, message, resolve }
    })
  }

  function answer(ok: boolean) {
    state.value.resolve?.(ok)
    state.value.open = false
  }

  return { state, confirm, answer }
}
