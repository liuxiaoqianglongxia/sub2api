<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('imageGeneration.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
            {{ t('imageGeneration.description') }}
          </p>
        </div>
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="loadingKeys"
          :title="t('common.refresh')"
          @click="loadKeys"
        >
          <Icon name="refresh" size="md" :class="loadingKeys ? 'animate-spin' : ''" />
        </button>
      </div>

      <div class="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(320px,420px)_1fr]">
        <section class="rounded-lg border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900">
          <div class="space-y-4">
            <div>
              <label class="input-label mb-1.5 block">{{ t('imageGeneration.apiKey') }}</label>
              <Select
                v-model="selectedKeyId"
                :options="keyOptions"
                :disabled="loadingKeys || keyOptions.length === 0"
                :placeholder="t('imageGeneration.selectKey')"
              />
            </div>

            <div>
              <label class="input-label mb-1.5 block">{{ t('imageGeneration.model') }}</label>
              <Select v-model="form.model" :options="modelOptions" />
            </div>

            <TextArea
              v-model="form.prompt"
              :label="t('imageGeneration.prompt')"
              :placeholder="t('imageGeneration.promptPlaceholder')"
              :rows="7"
              :error="promptError"
            />

            <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div>
                <label class="input-label mb-1.5 block">{{ t('imageGeneration.size') }}</label>
                <Select v-model="form.size" :options="sizeOptions" />
              </div>
              <div>
                <label class="input-label mb-1.5 block">{{ t('imageGeneration.quality') }}</label>
                <Select v-model="form.quality" :options="qualityOptions" />
              </div>
              <div>
                <label class="input-label mb-1.5 block">{{ t('imageGeneration.count') }}</label>
                <Select v-model="form.n" :options="countOptions" />
              </div>
            </div>

            <div v-if="keyOptions.length === 0" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800/60 dark:bg-amber-950/40 dark:text-amber-200">
              {{ loadingKeys ? t('imageGeneration.loadingKeys') : t('imageGeneration.noImageKeys') }}
            </div>

            <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-800/60 dark:bg-red-950/40 dark:text-red-200">
              {{ errorMessage }}
            </div>

            <button
              type="button"
              class="btn btn-primary w-full justify-center"
              :disabled="!canGenerate"
              @click="onGenerate"
            >
              <Icon v-if="generating" name="refresh" size="md" class="mr-2 animate-spin" />
              <Icon v-else name="sparkles" size="md" class="mr-2" />
              {{ generating ? t('imageGeneration.generating') : t('imageGeneration.generate') }}
            </button>
          </div>
        </section>

        <section class="min-h-[420px] rounded-lg border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-900">
          <div v-if="generating" class="flex h-full min-h-[360px] items-center justify-center">
            <LoadingSpinner />
          </div>

          <div v-else-if="images.length === 0" class="flex h-full min-h-[360px] items-center justify-center rounded-lg border border-dashed border-gray-200 text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            {{ t('imageGeneration.empty') }}
          </div>

          <div v-else class="grid grid-cols-1 gap-4 md:grid-cols-2 2xl:grid-cols-3">
            <figure
              v-for="(image, index) in images"
              :key="`${image.src}-${index}`"
              class="overflow-hidden rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-950"
            >
              <img :src="image.src" :alt="image.alt" class="aspect-square w-full object-cover" />
              <figcaption v-if="image.revisedPrompt" class="border-t border-gray-200 p-3 text-xs leading-5 text-gray-500 dark:border-dark-700 dark:text-dark-300">
                {{ image.revisedPrompt }}
              </figcaption>
            </figure>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import { keysAPI } from '@/api/keys'
import { imageGenerationAPI, type ImageGenerationDataItem } from '@/api/imageGeneration'
import { useAppStore } from '@/stores'
import type { ApiKey, SelectOption } from '@/types'

interface GeneratedImage {
  src: string
  alt: string
  revisedPrompt?: string
}

const { t } = useI18n()
const appStore = useAppStore()

const loadingKeys = ref(false)
const generating = ref(false)
const errorMessage = ref('')
const promptError = ref('')
const apiKeys = ref<ApiKey[]>([])
const selectedKeyId = ref<number | null>(null)
const images = ref<GeneratedImage[]>([])

const form = reactive({
  model: 'gpt-image-2',
  prompt: '',
  size: '1024x1024',
  quality: 'auto',
  n: 1
})

const modelOptions: SelectOption[] = [
  { value: 'gpt-image-2', label: 'gpt-image-2' },
  { value: 'gpt-image-1.5', label: 'gpt-image-1.5' },
  { value: 'gpt-image-1', label: 'gpt-image-1' }
]

const sizeOptions: SelectOption[] = [
  { value: '1024x1024', label: '1024 x 1024' },
  { value: '1536x1024', label: '1536 x 1024' },
  { value: '1024x1536', label: '1024 x 1536' }
]

const qualityOptions: SelectOption[] = [
  { value: 'auto', label: t('imageGeneration.auto') },
  { value: 'low', label: t('imageGeneration.low') },
  { value: 'medium', label: t('imageGeneration.medium') },
  { value: 'high', label: t('imageGeneration.high') }
]

const countOptions: SelectOption[] = [
  { value: 1, label: '1' }
]

const imageCapableKeys = computed(() =>
  apiKeys.value.filter((key) => key.status === 'active' && key.group?.platform === 'openai' && key.group?.allow_image_generation)
)

const keyOptions = computed<SelectOption[]>(() =>
  imageCapableKeys.value.map((key) => ({
    value: key.id,
    label: key.group ? `${key.name} / ${key.group.name}` : key.name
  }))
)

const selectedKey = computed(() => imageCapableKeys.value.find((key) => key.id === selectedKeyId.value) || null)
const canGenerate = computed(() => Boolean(selectedKey.value && form.prompt.trim() && !generating.value))

watch(keyOptions, (options) => {
  if (!options.length) {
    selectedKeyId.value = null
    return
  }
  if (!options.some((option) => option.value === selectedKeyId.value)) {
    selectedKeyId.value = options[0].value as number
  }
}, { immediate: true })

watch(() => form.prompt, () => {
  if (promptError.value && form.prompt.trim()) {
    promptError.value = ''
  }
})

async function loadKeys() {
  loadingKeys.value = true
  errorMessage.value = ''
  try {
    const response = await keysAPI.list(1, 100, { status: 'active' })
    apiKeys.value = response.items || []
  } catch (error: any) {
    errorMessage.value = error?.message || t('imageGeneration.loadKeysFailed')
  } finally {
    loadingKeys.value = false
  }
}

function normalizeImage(item: ImageGenerationDataItem, index: number): GeneratedImage | null {
  if (item.b64_json) {
    return {
      src: `data:image/png;base64,${item.b64_json}`,
      alt: `${t('imageGeneration.resultAlt')} ${index + 1}`,
      revisedPrompt: item.revised_prompt
    }
  }
  if (item.url) {
    return {
      src: item.url,
      alt: `${t('imageGeneration.resultAlt')} ${index + 1}`,
      revisedPrompt: item.revised_prompt
    }
  }
  return null
}

async function onGenerate() {
  if (!form.prompt.trim()) {
    promptError.value = t('imageGeneration.promptRequired')
    return
  }
  if (!selectedKey.value) {
    errorMessage.value = t('imageGeneration.noSelectedKey')
    return
  }

  generating.value = true
  errorMessage.value = ''
  images.value = []
  try {
    const response = await imageGenerationAPI.generateImage(selectedKey.value.key, {
      model: form.model,
      prompt: form.prompt.trim(),
      size: form.size,
      quality: form.quality,
      n: Number(form.n),
      response_format: 'b64_json'
    })
    images.value = (response.data || [])
      .map((item, index) => normalizeImage(item, index))
      .filter((item): item is GeneratedImage => Boolean(item))
    if (!images.value.length) {
      errorMessage.value = t('imageGeneration.emptyResponse')
    } else {
      appStore.showToast('success', t('imageGeneration.generateSuccess'), 3000)
    }
  } catch (error: any) {
    errorMessage.value = error?.message || t('imageGeneration.generateFailed')
  } finally {
    generating.value = false
  }
}

onMounted(() => {
  loadKeys()
})
</script>
