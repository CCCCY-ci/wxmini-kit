<script setup lang="ts">
import Dialog from 'primevue/dialog';
import { ref, watch } from "vue";
import { ScanPathItem } from "../entries/entries";
import * as AppService from '../../wailsjs/go/main/AppService';
import type { PathScanResult } from '../../wailsjs/go/main/AppService';

interface FileFilter {
  DisplayName: string;
  Pattern: string;
}

const emit = defineEmits<{
  confirm: [path: ScanPathItem];
}>();

const visible = defineModel<boolean>('visible', { default: false });
const loading = ref(false);
const defaultPaths = ref<ScanPathItem[]>([]);
const activeTab = ref<'auto' | 'manual'>('auto');
const scanResult = ref<PathScanResult | null>(null);
const logsExpanded = ref(false);

function selectWxapkgFile() {
  AppService.OpenFileDialog("选择微信小程序文件(.wxapkg)", "", [
    { DisplayName: "微信小程序文件", Pattern: "*.wxapkg" },
    { DisplayName: "所有文件", Pattern: "*.*" }
  ] as FileFilter[]).then(path => {
    if (path) {
      const item = new ScanPathItem(path, false);
      emit('confirm', item);
      visible.value = false;
    }
  });
}

function selectAppDir() {
  AppService.OpenDirectoryDialog("选择小程序目录", "").then(path => {
    if (path) {
      const item = new ScanPathItem(path, false);
      emit('confirm', item);
      visible.value = false;
    }
  });
}

function selectInstallDir() {
  AppService.OpenDirectoryDialog("选择微信小程序安装目录", "").then(path => {
    if (path) {
      const item = new ScanPathItem(path, true);
      emit('confirm', item);
      visible.value = false;
    }
  });
}

function selectDefaultPath(path: ScanPathItem) {
  emit('confirm', path);
  visible.value = false;
}

function loadDefaultPaths() {
  loading.value = true;
  activeTab.value = 'auto';
  scanResult.value = null;
  logsExpanded.value = false;
  AppService.GetDefaultPaths()
    .then(result => {
      scanResult.value = result;
      defaultPaths.value = (result.Paths ?? []).map(p => new ScanPathItem(p, true));
    })
    .finally(() => {
      loading.value = false;
      if (defaultPaths.value.length === 0) {
        activeTab.value = 'manual';
      }
    });
}

watch(visible, (newValue) => {
  if (newValue) loadDefaultPaths();
})
</script>

<template>
  <Dialog
    v-model:visible="visible"
    modal
    header="扫描微信小程序"
    :style="{ width: '600px', minHeight: '500px' }"
    :closable="true"
    :autofocus="false"
  >
    <!-- Tab Navigation -->
    <div class="tabs">
      <button
        class="tab-btn"
        :class="{ active: activeTab === 'auto' }"
        @click="activeTab = 'auto'"
      >
        自动扫描
      </button>
      <button
        class="tab-btn"
        :class="{ active: activeTab === 'manual' }"
        @click="activeTab = 'manual'"
      >
        手动指定
      </button>
    </div>

    <!-- Auto Tab -->
    <div v-if="activeTab === 'auto'" class="tab-content">
      <div v-if="loading" class="state-center">
        <div class="state-icon">
          <i class="pi pi-spin pi-spinner"></i>
        </div>
        <p class="state-text">正在检测微信小程序安装目录...</p>
      </div>

      <!-- 有日志时在顶部显示（不论是否找到路径） -->
      <div class="log-box" v-if="!loading && scanResult?.Logs?.trim()">
        <button class="log-toggle" @click="logsExpanded = !logsExpanded">
          <i class="pi" :class="logsExpanded ? 'pi-chevron-up' : 'pi-chevron-down'"></i>
          {{ logsExpanded ? '收起' : '展开' }}检测日志 · 未找到目录？请先在微信中打开任意小程序触发目录创建
        </button>
        <pre v-if="logsExpanded" class="log-content">{{ scanResult.Logs }}</pre>
      </div>

      <div v-if="!loading && defaultPaths.length === 0 && !logsExpanded" class="state-center">
        <div class="state-icon warning">
          <i class="pi pi-exclamation-triangle"></i>
        </div>
        <p class="state-title">未找到微信小程序安装目录</p>
        <p class="state-text">请使用手动指定模式</p>
      </div>

      <div v-if="!loading && defaultPaths.length > 0" class="path-list">
        <div
          v-for="path in defaultPaths"
          :key="path.path"
          class="path-card"
          @click="selectDefaultPath(path)"
        >
          <div class="path-card-icon">
            <i class="pi pi-folder"></i>
          </div>
          <div class="path-card-body">
            <div class="path-card-path">{{ path.path }}</div>
          </div>
          <i class="pi pi-chevron-right path-card-arrow"></i>
        </div>
      </div>
    </div>

    <!-- Manual Tab -->
    <div v-else class="tab-content">
      <div class="option-grid">
        <!-- Install Dir -->
        <div class="option-card" @click="selectInstallDir">
          <div class="option-card-icon">
            <i class="pi pi-folder"></i>
          </div>
          <div class="option-card-title">小程序安装目录</div>
          <div class="option-card-desc">自动识别目录下所有小程序</div>
          <div class="option-card-badge green">自动获取密钥</div>
        </div>

        <!-- wxapkg File -->
        <div class="option-card" @click="selectWxapkgFile">
          <div class="option-card-icon">
            <i class="pi pi-file"></i>
          </div>
          <div class="option-card-title">单独 .wxapkg 文件</div>
          <div class="option-card-desc">选择单个文件</div>
          <div class="option-card-badge orange">需手动输入密钥</div>
        </div>

        <!-- App Directory -->
        <div class="option-card" @click="selectAppDir">
          <div class="option-card-icon">
            <i class="pi pi-folder-open"></i>
          </div>
          <div class="option-card-title">单个小程序目录</div>
          <div class="option-card-desc">选择小程序文件夹</div>
          <div class="option-card-badge orange">需手动输入密钥</div>
        </div>
      </div>

      <div class="info-box" style="margin-top: 16px;">
        <div class="info-box-title">关于解密密钥</div>
        <div>
          密钥即小程序 ID，格式：<code>wx</code> + 16位字符，示例：<code>wxabcdef1234567890</code>
        </div>
        <div style="margin-top: 8px; color: var(--color-text-tertiary);">
          选择"小程序安装目录"会自动识别小程序 ID 作为密钥，其他模式需要在解包时手动输入
        </div>
      </div>
    </div>
  </Dialog>
</template>

<style scoped>
.tab-content {
  min-height: 260px;
}

.state-center {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 240px;
  gap: 10px;
}

.state-icon {
  font-size: 36px;
  color: var(--color-apple-blue);
  margin-bottom: 4px;
}

.state-icon.warning {
  color: #ff9500;
}

.state-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--color-near-black);
}

.state-text {
  font-size: 13px;
  color: var(--color-text-tertiary);
}

.path-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-height: 280px;
  overflow-y: auto;
}

/* 日志区域 */
.log-box {
  width: 100%;
  margin-top: 8px;
  margin-bottom: 8px;
  background: var(--color-light-gray);
  border-radius: var(--radius-standard);
  overflow: hidden;
}

.log-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 8px 12px;
  background: transparent;
  border: none;
  font-family: var(--font-text);
  font-size: 12px;
  color: var(--color-text-tertiary);
  cursor: pointer;
  transition: color 0.15s;
}
.log-toggle:hover {
  color: var(--color-apple-blue);
}
.log-toggle i {
  font-size: 11px;
}

.log-content {
  padding: 10px 12px 14px;
  font-family: "JetBrains Mono", "Cascadia Code", "Consolas", monospace;
  font-size: 12px;
  color: var(--color-text-secondary);
  line-height: 1.6;
  white-space: pre;
  overflow-x: auto;
  max-height: 220px;
  overflow-y: auto;
  border-top: 1px solid rgba(0, 0, 0, 0.06);
  margin: 0;
}
</style>
