import { ref, shallowRef } from 'vue'
import type { AggregatedModel } from '@/composables/useModelAggregation'

export const isOpen = ref(false)
export const currentModel = shallowRef<AggregatedModel | null>(null)

let previousBodyOverflow: string | null = null

export function open(model: AggregatedModel): void {
  if (!isOpen.value) {
    previousBodyOverflow = document.body.style.overflow
  }
  currentModel.value = model
  isOpen.value = true
  document.body.style.overflow = 'hidden'
}

export function close(): void {
  currentModel.value = null
  isOpen.value = false
  document.body.style.overflow = previousBodyOverflow ?? ''
  previousBodyOverflow = null
}

export function useModelDetailModal() {
  return {
    isOpen,
    currentModel,
    open,
    close
  }
}
