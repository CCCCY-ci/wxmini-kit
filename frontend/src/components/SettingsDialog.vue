<script setup lang="ts">
import { ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import * as AppService from '../../wailsjs/go/main/AppService'

export interface UnpackSettings {
  defaultOutputDir: string
  enableDecrypt: boolean
  enableJsonBeautify: boolean
  enableHtmlBeautify: boolean
  enableJsBeautify: boolean
}

const props = defineProps<UnpackSettings>()

const emit = defineEmits<{
  save: [settings: UnpackSettings]
}>()

const visible = defineModel<boolean>('visible', { default: false })
const draft = ref<UnpackSettings>({
  defaultOutputDir: '',
  enableDecrypt: true,
  enableJsonBeautify: true,
  enableHtmlBeautify: false,
  enableJsBeautify: false,
})

function resetDraft() {
  draft.value = {
    defaultOutputDir: props.defaultOutputDir ?? '',
    enableDecrypt: props.enableDecrypt,
    enableJsonBeautify: props.enableJsonBeautify,
    enableHtmlBeautify: props.enableHtmlBeautify,
    enableJsBeautify: props.enableJsBeautify,
  }
}

watch(visible, (newValue) => {
  if (newValue) resetDraft()
})

async function selectDirectory() {
  try {
    const path = await AppService.OpenDirectoryDialog('选择默认输出目录', draft.value.defaultOutputDir)
    if (path) draft.value.defaultOutputDir = path
  } catch {
    // The native dialog can be cancelled or unavailable during app shutdown.
  }
}

function saveSettings() {
  emit('save', {
    ...draft.value,
    defaultOutputDir: draft.value.defaultOutputDir.trim(),
  })
  visible.value = false
}
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="设置"
    :style="{ width: '520px' }"
    :autofocus="false"
  >
    <div class="form-section">
      <div class="section-label">默认输出目录</div>
      <div class="form-input-wrapper">
        <input
          v-model="draft.defaultOutputDir"
          class="form-input"
          type="text"
          placeholder="可手动输入，或点击右侧图标选择目录"
          style="width:100%; padding-right:40px"
        />
        <i
          class="pi pi-folder form-input-icon-right"
          style="pointer-events:auto; cursor:pointer"
          @click="selectDirectory"
        ></i>
      </div>
      <p class="form-hint">
        每次解包都会在这里创建新的输出目录；已有目录不会被覆盖。
      </p>
    </div>

    <div class="settings-section">
      <div class="section-label">解包选项</div>
      <label class="checkbox-row">
        <input v-model="draft.enableDecrypt" type="checkbox" />
        <span class="checkbox-row-label">解密加密的小程序包</span>
      </label>
      <label class="checkbox-row">
        <input v-model="draft.enableJsonBeautify" type="checkbox" />
        <span class="checkbox-row-label">美化 JSON</span>
      </label>
      <label class="checkbox-row">
        <input v-model="draft.enableHtmlBeautify" type="checkbox" />
        <span class="checkbox-row-label">美化 HTML</span>
      </label>
      <label class="checkbox-row">
        <input v-model="draft.enableJsBeautify" type="checkbox" />
        <span class="checkbox-row-label">美化 JavaScript</span>
      </label>
      <p class="form-hint">
        全量美化会增加处理时间和 CPU 使用；需要时再打开即可。
      </p>
    </div>

    <div class="dialog-footer" style="padding: 16px 0 0; border: none; justify-content:flex-end; gap:8px">
      <button class="btn-secondary" @click="visible = false">取消</button>
      <button class="btn-primary" @click="saveSettings">保存</button>
    </div>
  </Dialog>
</template>

<style scoped>
.form-section,
.settings-section {
  margin-bottom: 20px;
}

.settings-section {
  padding-top: 4px;
  border-top: 1px solid rgba(0, 0, 0, 0.07);
}

.settings-section .section-label {
  margin-top: 18px;
}

.checkbox-row + .checkbox-row {
  margin-top: 11px;
}

.form-hint {
  font-size: 12px;
  line-height: 1.5;
  color: var(--color-text-tertiary);
  margin-top: 8px;
}
</style>
