<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Toast from 'primevue/toast'
import SettingsDialog from './components/SettingsDialog.vue'
import { EventUnpackProgress } from './entries/entries'
import { formatSize, formatTime, UnpackStatusType, useAppToast } from './entries/util'
import { wechat } from '../wailsjs/go/models'
import WxapkgItem = wechat.WxapkgItem
import UnpackOptions = wechat.UnpackOptions
import * as AppService from '../wailsjs/go/main/AppService'

interface AppSettings {
  defaultOutputDir: string
  enableDecrypt: boolean
  enableJsonBeautify: boolean
  enableHtmlBeautify: boolean
  enableJsBeautify: boolean
}

interface StoredSettings extends Partial<AppSettings> {
  lastOutputDir?: string
}

interface ScanPathOption {
  path: string
  scan: boolean
  accountId: string
  label: string
}

const SETTINGS_STORAGE_KEY = 'wxapkg:output-directory:preference:v1'

function createDefaultSettings(): AppSettings {
  return {
    defaultOutputDir: '',
    enableDecrypt: true,
    enableJsonBeautify: true,
    enableHtmlBeautify: false,
    enableJsBeautify: false,
  }
}

const settings = ref<AppSettings>(createDefaultSettings())
const settingsDialogVisible = ref(false)
const pendingUnpackItem = ref<WxapkgItem | null>(null)
const customOutputDirs = ref<Record<string, string>>({})
const detectedPaths = ref<ScanPathOption[]>([])
const selectedPathIndex = ref(0)
const pathLoading = ref(false)
const scanning = ref(false)
const scanError = ref('')
const wxapkgItems = ref<WxapkgItem[]>([])
const scanCompleted = ref(false)
const search = ref('')
const toast = useAppToast()
const version = ref('v0.1.0')
const github = ref('https://github.com/CCCCY-ci/wxmini-kit')

const selectedPath = computed(() => detectedPaths.value[selectedPathIndex.value] ?? null)
const hasScanned = computed(() => wxapkgItems.value.length > 0)

const filteredItems = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return wxapkgItems.value
  return wxapkgItems.value.filter((item) =>
    item.WxId.toLowerCase().includes(query) ||
    (item.AppName ?? '').toLowerCase().includes(query)
  )
})

const appNameSourceLabels: Record<string, string> = {
  'local-metadata': '本地元数据',
  'package-config': '包内配置',
  'navigation-title': '导航标题',
  'code-candidate': '代码候选',
}

function appNameSourceLabel(source?: string): string {
  return source ? (appNameSourceLabels[source] ?? source) : ''
}

function appNameTooltip(item: WxapkgItem): string {
  if (!item.AppName) return '未从可靠的小程序元数据中识别到名称'
  const source = appNameSourceLabel(item.AppNameSource)
  return source ? `名称来源：${source}` : '名称来源：包内元数据'
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) return error.message
  if (typeof error === 'string') return error
  return '发生未知错误'
}

function normalizedPath(path: string): string {
  return path.replaceAll('\\', '/')
}

function accountIdFromPath(path: string): string {
  const matches = [...normalizedPath(path).matchAll(/(?:^|\/)users\/([^/]+)\/applet\/packages(?:\/|$)/gi)]
  return matches.at(-1)?.[1] ?? ''
}

function pathBaseName(path: string): string {
  const parts = normalizedPath(path).split('/').filter(Boolean)
  return parts.at(-1) ?? ''
}

function pathLabel(path: string, index: number): string {
  const accountId = accountIdFromPath(path)
  if (accountId) return accountId

  const baseName = pathBaseName(path)
  if (baseName && baseName.toLowerCase() !== 'packages') return baseName
  return `微信路径 ${index + 1}`
}

function makePathOption(path: string, scan: boolean, index: number): ScanPathOption {
  return {
    path,
    scan,
    accountId: accountIdFromPath(path),
    label: pathLabel(path, index),
  }
}

function pathListFromResult(result: unknown): string[] {
  if (Array.isArray(result)) {
    return result.filter((path): path is string => typeof path === 'string' && path.length > 0)
  }

  if (result && typeof result === 'object') {
    const paths = (result as { Paths?: unknown }).Paths
    if (Array.isArray(paths)) {
      return paths.filter((path): path is string => typeof path === 'string' && path.length > 0)
    }
  }
  return []
}

function resetPackageList() {
  wxapkgItems.value = []
  scanCompleted.value = false
  search.value = ''
  pendingUnpackItem.value = null
  customOutputDirs.value = {}
}

async function detectPaths() {
  if (pathLoading.value) return
  pathLoading.value = true
  scanError.value = ''
  try {
    const result = await AppService.GetDefaultPaths()
    const paths = [...new Set(pathListFromResult(result))]
    detectedPaths.value = paths.map((path, index) => makePathOption(path, true, index))
    selectedPathIndex.value = 0
    resetPackageList()
  } catch (error) {
    detectedPaths.value = []
    selectedPathIndex.value = 0
    scanError.value = `检测微信目录失败：${errorMessage(error)}`
  } finally {
    pathLoading.value = false
  }
}

function onPathChanged() {
  scanError.value = ''
  resetPackageList()
}

async function scanSelectedPath() {
  const option = selectedPath.value
  if (!option || scanning.value || pathLoading.value) return

  scanning.value = true
  scanError.value = ''
  try {
    wxapkgItems.value = (await AppService.ScanWxapkgItem(option.path, option.scan)) ?? []
    scanCompleted.value = true
    toast.info('扫描完成', `发现 ${wxapkgItems.value.length} 个小程序`)
  } catch (error) {
    wxapkgItems.value = []
    scanCompleted.value = false
    scanError.value = `扫描失败：${errorMessage(error)}`
    toast.error('扫描失败', errorMessage(error))
  } finally {
    scanning.value = false
  }
}

async function chooseDirectory() {
  try {
    const path = await AppService.OpenDirectoryDialog('选择小程序目录', selectedPath.value?.path ?? '')
    if (!path) return

    const baseName = pathBaseName(path).toLowerCase()
    const isSingleMiniProgram = /^wx[0-9a-f]{16}$/.test(baseName)
    detectedPaths.value = [makePathOption(path, !isSingleMiniProgram, 0)]
    selectedPathIndex.value = 0
    scanError.value = ''
    resetPackageList()
  } catch (error) {
    scanError.value = `选择目录失败：${errorMessage(error)}`
  }
}

async function chooseFile() {
  try {
    const path = await AppService.OpenFileDialog(
      '选择 wxapkg 文件',
      selectedPath.value?.path ?? '',
      [{ DisplayName: '微信小程序包 (*.wxapkg)', Pattern: '*.wxapkg' }]
    )
    if (!path) return

    detectedPaths.value = [makePathOption(path, false, 0)]
    selectedPathIndex.value = 0
    scanError.value = ''
    resetPackageList()
  } catch (error) {
    scanError.value = `选择文件失败：${errorMessage(error)}`
  }
}

function loadSettings() {
  const defaults = createDefaultSettings()
  try {
    const raw = localStorage.getItem(SETTINGS_STORAGE_KEY)
    if (!raw) {
      settings.value = defaults
      return
    }

    const stored = JSON.parse(raw) as StoredSettings
    settings.value = {
      defaultOutputDir: typeof stored.defaultOutputDir === 'string' ? stored.defaultOutputDir.trim() : defaults.defaultOutputDir,
      enableDecrypt: typeof stored.enableDecrypt === 'boolean' ? stored.enableDecrypt : defaults.enableDecrypt,
      enableJsonBeautify: typeof stored.enableJsonBeautify === 'boolean' ? stored.enableJsonBeautify : defaults.enableJsonBeautify,
      enableHtmlBeautify: typeof stored.enableHtmlBeautify === 'boolean' ? stored.enableHtmlBeautify : defaults.enableHtmlBeautify,
      enableJsBeautify: typeof stored.enableJsBeautify === 'boolean' ? stored.enableJsBeautify : defaults.enableJsBeautify,
    }
  } catch {
    settings.value = defaults
  }
}

function persistSettings() {
  localStorage.setItem(SETTINGS_STORAGE_KEY, JSON.stringify(settings.value))
}

function saveSettings(next: AppSettings) {
  settings.value = { ...next, defaultOutputDir: next.defaultOutputDir.trim() }
  persistSettings()

  const pending = pendingUnpackItem.value
  if (pending && settings.value.defaultOutputDir) {
    pendingUnpackItem.value = null
    void unpack(pending)
  }
}

function replaceItem(item: WxapkgItem) {
  wxapkgItems.value = wxapkgItems.value.map((current) =>
    current.UUID === item.UUID ? item : current
  )
}

function updateItem(uuid: string, patch: Partial<WxapkgItem>) {
  wxapkgItems.value = wxapkgItems.value.map((item) =>
    item.UUID === uuid ? ({ ...item, ...patch } as WxapkgItem) : item
  )
}

function outputDirectoryFor(item: WxapkgItem): string {
  return customOutputDirs.value[item.Location]?.trim() || settings.value.defaultOutputDir.trim()
}

async function changeOutputDirectory(item: WxapkgItem) {
  if (item.UnpackStatus === UnpackStatusType.Running) return
  try {
    const path = await AppService.OpenDirectoryDialog('选择解包输出目录', outputDirectoryFor(item))
    if (path) {
      customOutputDirs.value = { ...customOutputDirs.value, [item.Location]: path }
      toast.info('已设置单独目录', '本行解包时将使用该目录')
    }
  } catch (error) {
    toast.error('选择目录失败', errorMessage(error))
  }
}

async function unpack(item: WxapkgItem) {
  if (item.UnpackStatus === UnpackStatusType.Running) return

  const outputDir = outputDirectoryFor(item)
  if (!outputDir) {
    pendingUnpackItem.value = item
    settingsDialogVisible.value = true
    toast.warning('请先设置输出目录', '解包结果需要一个独立的输出目录')
    return
  }

  let savePath: string
  try {
    savePath = await AppService.ComputeSavePath(outputDir, item.Location)
  } catch (error) {
    toast.error('无法确定输出目录', errorMessage(error))
    return
  }

  const request = { ...item } as WxapkgItem
  const options = {
    EnableDecrypt: settings.value.enableDecrypt,
    EnableJsBeautify: settings.value.enableJsBeautify,
    EnableHtmlBeautify: settings.value.enableHtmlBeautify,
    EnableJsonBeautify: settings.value.enableJsonBeautify,
    OutputDir: outputDir,
    SavePath: savePath,
  } as UnpackOptions

  notifiedUuids.delete(item.UUID)
  replaceItem({
    ...item,
    UnpackStatus: UnpackStatusType.Running,
    UnpackProgress: 0,
    UnpackCurrent: 0,
    UnpackErrorMessage: '',
    UnpackSavePath: savePath,
  } as WxapkgItem)

  try {
    await AppService.UnpackWxapkgItem(request, options)
  } catch (error) {
    updateItem(item.UUID, {
      UnpackStatus: UnpackStatusType.Error,
      UnpackErrorMessage: errorMessage(error),
    })
    toast.error('解包失败', errorMessage(error))
  }
}

async function cancelUnpack(item: WxapkgItem) {
  if (item.UnpackStatus !== UnpackStatusType.Running) return
  try {
    const accepted = await AppService.CancelUnpack(item.UUID)
    if (!accepted) {
      toast.info('任务已结束', '当前解包任务没有可取消的后台操作')
    }
  } catch (error) {
    toast.error('取消解包失败', errorMessage(error))
  }
}

function openFolder(folder?: string) {
  if (!folder) {
    toast.error('无法打开目录', '没有可用的解包输出目录')
    return
  }
  AppService.OpenPath(folder).catch((error) => toast.error('打开目录失败', errorMessage(error)))
}

async function copyPackagePath(path: string) {
  const value = path.trim()
  if (!value) return
  try {
    await AppService.ClipboardSetText(value)
    toast.success('路径已复制', '完整小程序包路径已复制到剪贴板')
  } catch (error) {
    toast.error('复制路径失败', errorMessage(error))
  }
}

function clearList() {
  resetPackageList()
}

function openUrl(url: string) {
  void AppService.OpenUrl(url)
}

function statusText(item: WxapkgItem): string {
  if (item.UnpackStatus === UnpackStatusType.Running) return `解包中 ${Math.round(item.UnpackProgress || 0)}%`
  if (item.UnpackStatus === UnpackStatusType.Finished) return '已完成'
  if (item.UnpackStatus === UnpackStatusType.Error) return '失败'
  if (item.UnpackStatus === UnpackStatusType.Cancelled) return '已取消'
  return '待解包'
}

function actionText(item: WxapkgItem): string {
  if (item.UnpackStatus === UnpackStatusType.Finished) return '重新解包'
  if (item.UnpackStatus === UnpackStatusType.Error) return '重试'
  if (item.UnpackStatus === UnpackStatusType.Cancelled) return '重试'
  return '解包'
}

const notifiedUuids = new Set<string>()

function processProgress(uuid: string) {
  AppService.GetWxapkgItem(uuid).then((data) => {
    if (!data) return
    const current = wxapkgItems.value.find((item) => item.UUID === uuid)
    if (!current) return

    const updated = { ...current, ...data } as WxapkgItem
    replaceItem(updated)

    if (notifiedUuids.has(uuid)) return
    if (data.UnpackStatus === UnpackStatusType.Finished) {
      notifiedUuids.add(uuid)
      toast.success('解包完成', '可以通过列表行内的“打开目录”查看结果')
    } else if (data.UnpackStatus === UnpackStatusType.Error) {
      notifiedUuids.add(uuid)
      toast.error('解包失败', data.UnpackErrorMessage || '未提供错误信息')
    } else if (data.UnpackStatus === UnpackStatusType.Cancelled) {
      notifiedUuids.add(uuid)
      toast.info('已取消解包', '可以重新点击解包继续处理')
    }
  }).catch(() => {
    // A progress event can race with app shutdown; the next event will retry.
  })
}

onMounted(() => {
  loadSettings()
  void detectPaths()
  window.runtime.EventsOn(EventUnpackProgress, (uuid: unknown) => {
    if (typeof uuid === 'string') processProgress(uuid)
  })
  AppService.Version().then((value) => { version.value = value })
  AppService.Github().then((value) => { github.value = value })
})

onBeforeUnmount(() => {
  window.runtime.EventsOff(EventUnpackProgress)
})
</script>

<template>
  <div class="app-shell">
    <main class="content-area">
      <section class="scan-card">
        <div class="scan-card-header">
          <div>
            <div class="section-title">选择小程序路径</div>
            <div class="section-hint">启动时只检测小程序路径，不会自动扫描；点击扫描后才读取其中的小程序。</div>
          </div>
          <div class="scan-card-tools">
            <button class="btn-text" :disabled="pathLoading" @click="detectPaths">
              <i class="pi pi-refresh" :class="{ 'pi-spin': pathLoading }"></i>
              刷新路径
            </button>
            <button class="btn-text" @click="settingsDialogVisible = true">
              <i class="pi pi-cog"></i>
              设置
            </button>
            <button
              class="btn-primary scan-button"
              :disabled="!selectedPath || scanning || pathLoading"
              @click="scanSelectedPath"
            >
              <i class="pi" :class="scanning ? 'pi-spin pi-spinner' : 'pi-search'"></i>
              {{ scanning ? '扫描中...' : '扫描' }}
            </button>
          </div>
        </div>

        <div class="path-selector-row">
          <div class="select-wrapper">
            <i class="pi pi-folder-open select-icon"></i>
            <select
              v-model.number="selectedPathIndex"
              class="account-select"
              :disabled="pathLoading || detectedPaths.length === 0"
              @change="onPathChanged"
            >
              <option v-if="pathLoading" :value="0">正在检测小程序路径...</option>
              <option v-else-if="detectedPaths.length === 0" :value="0">未检测到小程序路径</option>
              <option v-for="(option, index) in detectedPaths" :key="option.path" :value="index">
                {{ option.label }}
              </option>
            </select>
            <i class="pi pi-chevron-down select-arrow"></i>
          </div>
          <span v-if="selectedPath" class="path-type">
            {{ selectedPath.scan ? '小程序路径' : '单个小程序包' }}
          </span>
          <span v-if="detectedPaths.length > 1" class="path-count">
            检测到 {{ detectedPaths.length }} 个小程序路径
          </span>
        </div>

        <div class="manual-actions">
          <span class="manual-label">未找到或要指定其他路径：</span>
          <button class="btn-text" @click="chooseDirectory">
            <i class="pi pi-folder-open"></i>
            选择目录
          </button>
          <button class="btn-text" @click="chooseFile">
            <i class="pi pi-file"></i>
            选择文件
          </button>
        </div>

        <div v-if="scanError" class="inline-error">
          <i class="pi pi-exclamation-circle"></i>
          <span>{{ scanError }}</span>
        </div>
      </section>

      <section class="list-card">
        <div class="list-toolbar">
          <div class="list-title-group">
            <div class="section-title">小程序列表</div>
            <span v-if="hasScanned" class="list-count">{{ wxapkgItems.length }}</span>
            <span v-else class="list-hint">{{ scanCompleted ? '当前目录没有小程序' : '选择路径后点击扫描' }}</span>
          </div>
          <div class="list-actions">
            <div v-if="wxapkgItems.length > 0" class="search-wrapper compact-search">
              <i class="pi pi-search search-icon"></i>
              <input v-model="search" class="search-input" type="text" placeholder="搜索名称或 ID" />
            </div>
            <button v-if="wxapkgItems.length > 0" class="btn-text" @click="clearList">
              <i class="pi pi-trash"></i>
              清空
            </button>
          </div>
        </div>

        <div v-if="filteredItems.length > 0" class="package-list">
          <article v-for="item in filteredItems" :key="item.UUID" class="package-row">
            <div class="package-row-main">
              <div class="package-name-block">
                <div
                  class="package-name"
                  :class="{ 'package-name-unknown': !item.AppName }"
                  :title="appNameTooltip(item)"
                >
                  {{ item.AppName || '未识别名称' }}
                </div>
                <div v-if="item.AppName && item.AppNameSource" class="package-source">
                  {{ appNameSourceLabel(item.AppNameSource) }}
                </div>
                <button
                  v-if="item.Location"
                  type="button"
                  class="package-path mono"
                  :title="item.Location"
                  @click.stop="copyPackagePath(item.Location)"
                >
                  <i class="pi pi-copy"></i>
                  <span>{{ normalizedPath(item.Location) }}</span>
                </button>
              </div>
              <div class="package-id mono">{{ item.WxId || '未知 ID' }}</div>
              <div class="package-meta">{{ formatTime(item.LastModifyTime, false) }}</div>
              <div class="package-meta package-size">{{ formatSize(item.Size) }}</div>
              <div class="package-status" :class="item.UnpackStatus || 'idle'">
                <span class="status-indicator"></span>
                {{ statusText(item) }}
              </div>
            </div>

            <div v-if="item.UnpackStatus === UnpackStatusType.Running" class="row-progress">
              <div class="progress-track">
                <div class="progress-fill" :style="{ width: `${Math.max(0, Math.min(100, item.UnpackProgress || 0))}%` }"></div>
              </div>
              <span class="progress-file">{{ item.UnpackCurrentFile || '正在准备文件...' }}</span>
            </div>

            <div v-if="item.UnpackStatus === UnpackStatusType.Error" class="row-error" :title="item.UnpackErrorMessage">
              <i class="pi pi-exclamation-circle"></i>
              {{ item.UnpackErrorMessage || '解包失败，可点击重试' }}
            </div>

            <div v-if="item.UnpackStatus === UnpackStatusType.Cancelled" class="row-cancelled">
              <i class="pi pi-ban"></i>
              解包已取消，可重新点击解包
            </div>

            <div class="package-row-actions">
              <button
                v-if="item.UnpackStatus === UnpackStatusType.Running"
                class="row-action row-action-cancel"
                @click="cancelUnpack(item)"
              >
                <i class="pi pi-times"></i>
                取消
              </button>
              <template v-else>
                <button
                  class="row-action"
                  :title="outputDirectoryFor(item) || '选择解包输出目录'"
                  @click="changeOutputDirectory(item)"
                >
                  <i class="pi pi-folder"></i>
                  输出
                </button>
                <button
                  class="row-action row-action-primary"
                  @click="unpack(item)"
                >
                  <i class="pi" :class="item.UnpackStatus === UnpackStatusType.Error ? 'pi-refresh' : 'pi-box'"></i>
                  {{ actionText(item) }}
                </button>
              </template>
              <button
                v-if="item.UnpackStatus === UnpackStatusType.Finished && item.UnpackSavePath"
                class="row-action"
                @click="openFolder(item.UnpackSavePath)"
              >
                <i class="pi pi-folder-open"></i>
                打开目录
              </button>
            </div>
          </article>
        </div>

        <div v-else class="list-empty">
          <i class="pi" :class="search ? 'pi-search' : 'pi-inbox'"></i>
          <div>{{ search ? `没有找到与“${search}”相关的小程序` : (scanCompleted ? '当前目录没有可识别的小程序' : '还没有扫描结果') }}</div>
          <small v-if="!search">{{ scanCompleted ? '可以选择其他账号目录或手动指定路径。' : '选择上方账号目录，然后点击扫描。' }}</small>
        </div>
      </section>
    </main>

    <footer class="footer">
      <span class="footer-disclaimer">仅供学习研究使用，请勿用于任何侵权或非法用途</span>
      <a class="footer-right" @click.prevent="openUrl(github)">
        <span class="footer-version">{{ version }}</span>
        <svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg" width="13" height="13">
          <path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.019 10.019 0 0022 12.017C22 6.484 17.522 2 12 2z"/>
        </svg>
      </a>
    </footer>

    <SettingsDialog
      v-model:visible="settingsDialogVisible"
      :default-output-dir="settings.defaultOutputDir"
      :enable-decrypt="settings.enableDecrypt"
      :enable-json-beautify="settings.enableJsonBeautify"
      :enable-html-beautify="settings.enableHtmlBeautify"
      :enable-js-beautify="settings.enableJsBeautify"
      @save="saveSettings"
    />
    <Toast position="bottom-right" />
  </div>
</template>

<style scoped>
.app-shell {
  display: flex;
  flex-direction: column;
  height: 100vh;
  overflow: hidden;
  background: var(--color-light-gray);
}

.section-hint,
.list-hint {
  color: var(--color-text-tertiary);
  font-size: 12px;
  line-height: 1.5;
}

.list-actions,
.manual-actions,
.path-selector-row,
.package-row-actions {
  display: flex;
  align-items: center;
}

.content-area {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 14px;
  min-height: 0;
  padding: 16px 24px;
  overflow: hidden;
}

.scan-card,
.list-card {
  background: var(--color-white);
  border-radius: var(--radius-standard);
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.06), 0 1px 8px rgba(0, 0, 0, 0.03);
}

.scan-card {
  padding: 18px 20px 14px;
  flex-shrink: 0;
}

.scan-card-header,
.list-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 16px;
}

.scan-card-tools {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  flex-shrink: 0;
}

.section-title {
  color: var(--color-text-secondary);
  font-size: 15px;
  font-weight: 600;
}

.section-hint { margin-top: 3px; }
.scan-button { min-width: 92px; flex-shrink: 0; }

.path-selector-row { gap: 10px; margin-top: 16px; }

.select-wrapper {
  position: relative;
  min-width: 280px;
  max-width: 480px;
  flex: 1;
}

.select-icon,
.select-arrow {
  position: absolute;
  z-index: 1;
  top: 50%;
  color: var(--color-text-tertiary);
  pointer-events: none;
  transform: translateY(-50%);
}

.select-icon { left: 13px; }
.select-arrow { right: 13px; font-size: 12px; }

.account-select {
  width: 100%;
  height: 38px;
  padding: 0 38px;
  appearance: none;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: var(--radius-standard);
  outline: none;
  background: var(--color-btn-light);
  color: var(--color-text-secondary);
  font-family: var(--font-text);
  font-size: 14px;
  cursor: pointer;
}

.account-select:focus {
  border-color: var(--color-apple-blue);
  box-shadow: 0 0 0 3px rgba(0, 113, 227, 0.12);
}

.account-select:disabled { cursor: not-allowed; opacity: 0.6; }

.path-type,
.path-count,
.list-count {
  color: var(--color-text-tertiary);
  font-size: 12px;
  white-space: nowrap;
}

.path-type {
  padding: 4px 9px;
  border-radius: var(--radius-pill);
  background: rgba(0, 113, 227, 0.08);
  color: var(--color-link-blue);
}

.manual-actions { gap: 10px; margin-top: 10px; }
.manual-label { color: var(--color-text-tertiary); font-size: 12px; }

.inline-error,
.row-error {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  color: #c62828;
  font-size: 12px;
  line-height: 1.5;
}

.inline-error { margin-top: 10px; }

.list-card {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.list-toolbar {
  padding: 14px 18px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.07);
  flex-shrink: 0;
}

.list-title-group { display: flex; align-items: baseline; gap: 8px; }
.list-actions { gap: 14px; }
.compact-search { width: 210px; flex: none; }
.compact-search .search-input { height: 32px; }

.package-list { flex: 1; overflow-y: auto; }

.package-row {
  position: relative;
  display: block;
  padding: 14px 18px 12px;
  border-bottom: 1px solid rgba(0, 0, 0, 0.055);
  transition: background 0.15s;
}

.package-row:last-child { border-bottom: none; }
.package-row:hover { background: rgba(0, 0, 0, 0.015); }

.package-row-main {
  display: grid;
  grid-template-columns: minmax(170px, 1.4fr) minmax(150px, 1.2fr) 155px 80px 100px;
  align-items: center;
  gap: 16px;
  min-width: 0;
  padding-right: 340px;
}

.package-name-block { min-width: 0; }
.package-name {
  overflow: hidden;
  color: var(--color-text-secondary);
  font-size: 14px;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.package-name-unknown { color: var(--color-text-tertiary); font-weight: 400; }

.package-source {
  margin-top: 3px;
  overflow: hidden;
  color: var(--color-text-tertiary);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.package-path {
  display: flex;
  align-items: center;
  gap: 5px;
  width: 100%;
  min-width: 0;
  margin-top: 4px;
  padding: 0;
  overflow: hidden;
  border: 0;
  background: transparent;
  color: var(--color-text-tertiary);
  font-size: 11px;
  text-align: left;
  cursor: pointer;
}

.package-path:hover {
  color: var(--color-link-blue);
}

.package-path .pi {
  flex: 0 0 auto;
  font-size: 11px;
  opacity: 0.7;
}

.package-path span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.package-id,
.package-meta {
  overflow: hidden;
  color: var(--color-text-tertiary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.package-size { text-align: right; }

.package-status {
  display: inline-flex;
  align-items: center;
  justify-self: start;
  gap: 6px;
  color: var(--color-text-tertiary);
  font-size: 12px;
  white-space: nowrap;
}

.status-indicator {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.2);
}

.package-status.running { color: var(--color-apple-blue); }
.package-status.running .status-indicator {
  background: var(--color-apple-blue);
  animation: pulse 1.2s ease-in-out infinite;
}
.package-status.finished { color: #2c9b4b; }
.package-status.finished .status-indicator { background: #34c759; }
.package-status.error { color: #c62828; }
.package-status.error .status-indicator { background: #ff3b30; }
.package-status.cancelled { color: #a06a00; }
.package-status.cancelled .status-indicator { background: #ff9500; }

.package-row-actions {
  position: absolute;
  top: 50%;
  right: 18px;
  gap: 8px;
  transform: translateY(-50%);
}

.row-action {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 30px;
  padding: 0 11px;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 7px;
  background: var(--color-white);
  color: var(--color-text-secondary);
  font-family: var(--font-text);
  font-size: 12px;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s, color 0.15s;
  white-space: nowrap;
}

.row-action:hover {
  border-color: rgba(0, 113, 227, 0.45);
  background: rgba(0, 113, 227, 0.05);
  color: var(--color-link-blue);
}

.row-action-primary {
  border-color: rgba(0, 113, 227, 0.2);
  background: rgba(0, 113, 227, 0.08);
  color: var(--color-link-blue);
}

.row-action-cancel {
  border-color: rgba(255, 59, 48, 0.25);
  background: rgba(255, 59, 48, 0.06);
  color: #c62828;
}

.row-progress {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
  padding-right: 340px;
}

.progress-track { flex: 1; min-width: 100px; }

.progress-file {
  width: 210px;
  overflow: hidden;
  color: var(--color-text-tertiary);
  font-family: "JetBrains Mono", "Cascadia Code", "Consolas", monospace;
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-error {
  margin-top: 9px;
  padding-right: 340px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-cancelled {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 9px;
  padding-right: 340px;
  color: #a06a00;
  font-size: 12px;
}

.list-empty {
  display: flex;
  flex: 1;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 180px;
  color: var(--color-text-tertiary);
  font-size: 14px;
}

.list-empty > i { margin-bottom: 4px; font-size: 28px; opacity: 0.45; }
.list-empty small { font-size: 12px; }

.footer {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 7px 24px;
  background: var(--color-white);
  border-top: 1px solid rgba(0, 0, 0, 0.05);
  flex-shrink: 0;
}

.footer-disclaimer {
  position: absolute;
  left: 50%;
  color: rgba(0, 0, 0, 0.42);
  font-size: 10px;
  white-space: nowrap;
  transform: translateX(-50%);
}

.footer-right {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  color: rgba(0, 0, 0, 0.48);
  text-decoration: none;
  cursor: pointer;
}

.footer-right:hover { color: var(--color-apple-blue); }
.footer-version { font-size: 10px; }

@keyframes pulse {
  0%, 100% { opacity: 0.35; }
  50% { opacity: 1; }
}

@media (max-width: 820px) {
  .content-area { padding-left: 14px; padding-right: 14px; }

  .package-row-main {
    grid-template-columns: minmax(150px, 1.5fr) minmax(130px, 1fr) 110px;
    padding-right: 0;
  }

  .package-size,
  .package-status { display: none; }

  .package-row-actions {
    position: static;
    justify-content: flex-start;
    margin-top: 10px;
    transform: none;
  }

  .row-progress,
  .row-error,
  .row-cancelled { padding-right: 0; }
  .progress-file { width: 140px; }
}

@media (max-width: 560px) {
  .scan-card-header,
  .list-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }

  .scan-card-tools,
  .list-actions { width: 100%; justify-content: flex-end; }
  .scan-card-tools { flex-wrap: wrap; }
  .scan-button { align-self: auto; }

  .path-selector-row {
    align-items: stretch;
    flex-direction: column;
  }

  .select-wrapper,
  .compact-search { width: 100%; max-width: none; }
  .footer-disclaimer { display: none; }
}
</style>
