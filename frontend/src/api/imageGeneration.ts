export interface ImageGenerationPayload {
  model: string
  prompt: string
  size: string
  quality: string
  n: number
  response_format: 'url' | 'b64_json'
}

export interface ImageGenerationDataItem {
  url?: string
  b64_json?: string
  revised_prompt?: string
}

export interface ImageGenerationResponse {
  created?: number
  data?: ImageGenerationDataItem[]
}

export async function generateImage(
  apiKey: string,
  payload: ImageGenerationPayload,
  options?: { signal?: AbortSignal }
): Promise<ImageGenerationResponse> {
  const response = await fetch('/v1/images/generations', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload),
    signal: options?.signal
  })

  const text = await response.text()
  let data: any = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { message: text }
    }
  }

  if (!response.ok) {
    const message =
      data?.error?.message ||
      data?.message ||
      data?.detail ||
      `${response.status} ${response.statusText}`
    throw new Error(message)
  }

  return data || {}
}

export const imageGenerationAPI = {
  generateImage
}

export default imageGenerationAPI
