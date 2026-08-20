<script setup lang="ts">
import Dialog from 'primevue/dialog';
import { computed, reactive, ref, watch } from "vue";
import * as AppService from '../../wailsjs/go/main/AppService';
import { UnpackStatusType } from "../entries/util";
import {wechat} from "../../wailsjs/go/models";
import UnpackOptions = wechat.UnpackOptions;
import WxapkgItem = wechat.WxapkgItem;

type DialogStage = 'config' | 'progress' | 'complete' | 'error';

interface OutputDirSelection {
  outputDir: string;
  setAsDefault: boolean;
  useInSession: boolean;
}

const props = withDefaults(defineProps<{
  defaultOutputDir?: string;
  lastOutputDir?: string;
  sessionOutputDir?: string;
}>(), {
  defaultOutputDir: '',
  lastOutputDir: '',
  sessionOutputDir: '',
});

const emit = defineEmits<{
  confirm: [options: UnpackOptions];
  afterHide: [];
  openDirectory: [path: string];
  outputDirSelected: [payload: OutputDirSelection];
}>();

const visible = defineModel<boolean>('visible', { default: false });
const item = defineModel<WxapkgItem | null>('item', { default: null });

const options = reactive<UnpackOptions>({
  EnableDecrypt: true,
  EnableHtmlBeautify: false,
  EnableJsBeautify: false,
  EnableJsonBeautify: true,
  OutputDir: "",
  SavePath: "",
});

const decryptKey = ref('');
const currentStage = ref<DialogStage>('config');
const currentProgress = ref<WxapkgItem | null>(null);
const actualOutputDir = ref('');
const setAsDefaultOutputDir = ref(false);
const useOutputDirInSession = ref(false);

const normalizedDefaultOutputDir = computed(() => normalizeDir(props.defaultOutputDir));
const shouldShowOutputDirOptions = computed(() => {
  if (!options.OutputDir) {
    return false;
  }
  return normalizeDir(options.OutputDir) !== normalizedDefaultOutputDir.value;
});

function isWindowsPlatform(): boolean {
  return typeof navigator !== 'undefined' && /windows/i.test(navigator.userAgent);
}

function normalizeDir(path: string): string {
  const normalizedPath = path.trim().replace(/[\\/]+/g, '/').replace(/\/+$/, '');
  return isWindowsPlatform() ? normalizedPath.toLowerCase() : normalizedPath;
}

function getInitialOutputDir(): string {
  return props.sessionOutputDir.trim()
    || props.defaultOutputDir.trim()
    || props.lastOutputDir.trim()
    || '';
}

watch(() => [options.OutputDir, item.value?.Location], async () => {
  if (options.OutputDir && item.value) {
    actualOutputDir.value = await AppService.ComputeSavePath(options.OutputDir, item.value.Location)
  } else {
    actualOutputDir.value = ''
  }
}, { immediate: true })

watch(visible, (newVal) => {
  if (newVal && item.value) {
    currentStage.value = 'config';
    currentProgress.value = item.value;
    options.OutputDir = getInitialOutputDir();
    options.EnableDecrypt = true;
    setAsDefaultOutputDir.value = false;
    useOutputDirInSession.value = false;

    if (item.value.WxId && item.value.WxId.startsWith('wx')) {
      decryptKey.value = item.value.WxId;
    } else if (item.value.EncryptKey) {
      decryptKey.value = item.value.EncryptKey;
    } else {
      decryptKey.value = '';
    }

    if (item.value.UnpackStatus === UnpackStatusType.Running) {
      currentStage.value = 'progress';
    } else if (item.value.UnpackStatus === UnpackStatusType.Finished) {
      currentStage.value = 'complete';
    } else if (item.value.UnpackStatus === UnpackStatusType.Error) {
      currentStage.value = 'error';
    }
  }
});

watch(() => options.OutputDir, () => {
  if (!shouldShowOutputDirOptions.value) {
    setAsDefaultOutputDir.value = false;
    useOutputDirInSession.value = false;
  }
});

watch(() => item.value, (newItem) => {
  if (newItem && visible.value) {
    currentProgress.value = newItem
    if (newItem.UnpackStatus === UnpackStatusType.Finished && currentStage.value === 'progress') {
      currentStage.value = 'complete';
    } else if (newItem.UnpackStatus === UnpackStatusType.Error && currentStage.value === 'progress') {
      currentStage.value = 'error';
    }
  }
}, { deep: true, immediate: true })

function selectFolder() {
  AppService.OpenDirectoryDialog("选择输出目录", options.OutputDir).then((result) => {
    options.OutputDir = result;
  });
}

async function startUnpack() {
  if (!item.value) {
    return;
  }

  if (decryptKey.value) {
    item.value.EncryptKey = decryptKey.value;
  }

  const outputDir = options.OutputDir.trim();
  let savePath = actualOutputDir.value;
  if (!savePath) {
    savePath = await AppService.ComputeSavePath(outputDir, item.value.Location);
  }

  emit('outputDirSelected', {
    outputDir,
    setAsDefault: shouldShowOutputDirOptions.value && setAsDefaultOutputDir.value,
    useInSession: shouldShowOutputDirOptions.value && useOutputDirInSession.value,
  });

  options.OutputDir = outputDir;
  options.SavePath = savePath;
  emit('confirm', { ...options });
  currentStage.value = 'progress';
}

function openOutputDirectory() {
  if (currentProgress.value?.UnpackSavePath) {
    emit('openDirectory', currentProgress.value.UnpackSavePath);
  }
}

function closeDialog() {
  if (item.value && currentProgress.value) {
    Object.assign(item.value, currentProgress.value)
  }
  visible.value = false;
}

function minimizeDialog() {
  visible.value = false;
}
</script>

<template>
  <!-- Stage 1: Configuration -->
  <Dialog
    v-if="currentStage === 'config'"
    v-model:visible="visible"
    modal
    header="解包配置"
    :style="{ width: '500px' }"
    :autofocus="false"
    @after-hide="emit('afterHide')"
  >
    <div class="form-section">
      <div class="section-label">解密</div>
      <div class="form-row-check">
        <label class="checkbox-row">
          <input type="checkbox" v-model="options.EnableDecrypt" />
          <span class="checkbox-row-label">启用解密</span>
        </label>
      </div>
      <div v-if="options.EnableDecrypt" class="mt-3">
        <input
          v-model="decryptKey"
          class="form-input mono"
          type="text"
          placeholder="小程序ID，如：wxabcdef1234567890"
          style="width:100%"
        />
        <p class="form-hint">密钥即小程序 ID，格式：wx 开头 + 16位字符</p>
      </div>
    </div>

    <div class="form-section">
      <div class="section-label">代码美化</div>
      <div class="check-group">
        <label class="checkbox-row">
          <input type="checkbox" v-model="options.EnableJsonBeautify" />
          <span class="checkbox-row-label">JSON</span>
        </label>
        <label class="checkbox-row">
          <input type="checkbox" v-model="options.EnableHtmlBeautify" />
          <span class="checkbox-row-label">HTML</span>
        </label>
        <label class="checkbox-row">
          <input type="checkbox" v-model="options.EnableJsBeautify" />
          <span class="checkbox-row-label">JavaScript</span>
        </label>
      </div>
    </div>

    <div class="form-section">
      <div class="section-label">输出目录</div>
      <div class="form-input-wrapper">
        <input
          v-model="options.OutputDir"
          id="outputDir"
          class="form-input"
          type="text"
          placeholder="可手动输入，或点击右侧图标选择输出目录"
          style="width:100%; padding-right:40px"
        />
        <i
          class="pi pi-folder form-input-icon-right"
          style="pointer-events:auto; cursor:pointer"
          @click="selectFolder"
        ></i>
      </div>
      <p class="form-hint path-hint" v-if="options.OutputDir">
        文件将保存到：<br><span class="path-break">{{ actualOutputDir }}</span>
      </p>
    </div>

    <div class="form-section" v-if="shouldShowOutputDirOptions">
      <div class="check-group-column">
        <label class="checkbox-row">
          <input type="checkbox" v-model="setAsDefaultOutputDir" />
          <span class="checkbox-row-label">将当前目录作为默认目录</span>
        </label>
        <label class="checkbox-row">
          <input type="checkbox" v-model="useOutputDirInSession" />
          <span class="checkbox-row-label">下次也使用该目录输出（本次会话有效）</span>
        </label>
      </div>
    </div>

    <div class="dialog-footer" style="padding: 16px 0 0; border: none; justify-content:flex-end; gap:8px">
      <button class="btn-secondary" @click="closeDialog">取消</button>
      <button
        class="btn-primary"
        :disabled="!options.OutputDir.trim() || (options.EnableDecrypt && !decryptKey)"
        @click="startUnpack"
      >
        开始解包
      </button>
    </div>
  </Dialog>

  <!-- Stage 2: Progress -->
  <Dialog
    v-else-if="currentStage === 'progress'"
    v-model:visible="visible"
    modal
    header="解包进行中"
    :style="{ width: '500px' }"
    :closable="false"
    :autofocus="false"
  >
    <div class="form-section">
      <div class="progress-header">
        <span class="progress-label">总进度</span>
        <span class="progress-pct">{{ Math.round(currentProgress?.UnpackProgress ?? 0) }}%</span>
      </div>
      <div class="progress-track">
        <div class="progress-fill" :style="{ width: (currentProgress?.UnpackProgress ?? 0) + '%' }"></div>
      </div>
      <div class="progress-sub">
        {{ currentProgress?.UnpackCurrent }} / {{ currentProgress?.UnpackTotal }} 个文件
      </div>
    </div>

    <div class="progress-file" v-if="currentProgress?.UnpackCurrentFile">
      <div class="progress-file-label">当前文件</div>
      <div class="progress-file-name mono">{{ currentProgress.UnpackCurrentFile }}</div>
    </div>

    <div class="dialog-footer" style="padding: 16px 0 0; border: none; justify-content:flex-end">
      <button class="btn-secondary" @click="minimizeDialog">后台运行</button>
    </div>
  </Dialog>

  <!-- Stage 3: Complete -->
  <Dialog
    v-else-if="currentStage === 'complete'"
    v-model:visible="visible"
    modal
    header="解包完成"
    :style="{ width: '500px' }"
    :autofocus="false"
  >
    <div class="result-center">
      <span class="result-icon success">
        <i class="pi pi-check-circle"></i>
      </span>
      <div class="result-title">解包成功完成</div>
      <div class="result-subtitle" v-if="currentProgress">
        已解包 {{ currentProgress.UnpackTotal }} 个文件
      </div>
      <div
        class="result-subtitle result-restore-summary"
        v-if="currentProgress && currentProgress.UnpackRestoredFiles > 0"
      >
        已还原 {{ currentProgress.UnpackRestoredFiles }} 个源码文件
      </div>
      <div
        class="result-subtitle result-restore-warning"
        v-if="currentProgress && currentProgress.UnpackRestoreWarnings > 0"
      >
        有 {{ currentProgress.UnpackRestoreWarnings }} 项动态表达式未能静态还原，原始 runtime 已保存到 .wxapkg-runtime
      </div>
    </div>

    <div class="result-path" v-if="currentProgress?.UnpackSavePath">
      <div class="result-path-label">输出目录</div>
      <div class="result-path-value mono">{{ currentProgress.UnpackSavePath }}</div>
    </div>

    <div class="dialog-footer" style="padding: 16px 0 0; border: none; justify-content:flex-end; gap:8px">
      <button class="btn-secondary" @click="closeDialog">关闭</button>
      <button class="btn-primary" @click="openOutputDirectory">
        <i class="pi pi-folder"></i>
        打开目录
      </button>
    </div>
  </Dialog>

  <!-- Stage 4: Error -->
  <Dialog
    v-else-if="currentStage === 'error'"
    v-model:visible="visible"
    modal
    header="解包失败"
    :style="{ width: '500px' }"
    :autofocus="false"
  >
    <div class="result-center">
      <span class="result-icon error">
        <i class="pi pi-exclamation-circle"></i>
      </span>
      <div class="result-title">解包过程中出现错误</div>
      <div class="result-subtitle" style="max-width:320px; margin-top:8px" v-if="currentProgress?.UnpackErrorMessage">
        {{ currentProgress.UnpackErrorMessage }}
      </div>
    </div>

    <div class="result-path" v-if="currentProgress?.UnpackSavePath">
      <div class="result-path-label">部分输出目录</div>
      <div class="result-path-value mono">{{ currentProgress.UnpackSavePath }}</div>
    </div>

    <div class="dialog-footer" style="padding: 16px 0 0; border: none; justify-content:flex-end">
      <button class="btn-secondary" @click="closeDialog">关闭</button>
    </div>
  </Dialog>
</template>

<style scoped>
.form-section {
  margin-bottom: 20px;
}

.form-row-check {
  margin-bottom: 8px;
}

.check-group {
  display: flex;
  gap: 20px;
  flex-wrap: wrap;
}

.check-group-column {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.form-hint {
  font-size: 12px;
  color: var(--color-text-tertiary);
  margin-top: 6px;
  line-height: 1.4;
  word-break: normal;
  overflow-wrap: normal;
}

.path-break {
  font-size: 12px;
  word-break: break-all;
  overflow-wrap: anywhere;
}

.mono {
  font-family: "JetBrains Mono", "Cascadia Code", "Consolas", monospace;
}

.mt-3 {
  margin-top: 10px;
}

/* Progress */
.progress-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 8px;
}
.progress-label {
  font-size: 13px;
  color: var(--color-text-secondary);
}
.progress-pct {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-near-black);
}
.progress-sub {
  font-size: 12px;
  color: var(--color-text-tertiary);
  text-align: right;
  margin-top: 6px;
}

.progress-file {
  margin-top: 16px;
  background: var(--color-light-gray);
  border-radius: var(--radius-standard);
  padding: 12px 14px;
}
.progress-file-label {
  font-size: 12px;
  color: var(--color-text-tertiary);
  margin-bottom: 4px;
}
.progress-file-name {
  font-size: 13px;
  color: var(--color-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Result */
.result-center {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 8px 0 16px;
}

.result-icon {
  font-size: 64px;
  line-height: 1;
  margin-bottom: 12px;
  display: block;
}
.result-icon.success { color: #34c759; }
.result-icon.error { color: #ff3b30; }

.result-title {
  font-family: var(--font-display);
  font-size: 20px;
  font-weight: 600;
  color: var(--color-near-black);
  margin-bottom: 4px;
  letter-spacing: -0.02em;
}

.result-subtitle {
  font-size: 13px;
  color: var(--color-text-tertiary);
}

.result-restore-summary {
  margin-top: 6px;
  color: #2c9b4b;
}

.result-restore-warning {
  max-width: 420px;
  margin-top: 8px;
  color: #a06a00;
  text-align: center;
}

.result-path {
  background: var(--color-light-gray);
  border-radius: var(--radius-standard);
  padding: 12px 14px;
  margin-bottom: 4px;
}
.result-path-label {
  font-size: 12px;
  color: var(--color-text-tertiary);
  margin-bottom: 4px;
}
.result-path-value {
  font-size: 13px;
  color: var(--color-text-secondary);
  word-break: break-all;
  overflow-wrap: anywhere;
}
</style>
