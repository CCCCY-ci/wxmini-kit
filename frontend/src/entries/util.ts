import {useToast} from 'primevue/usetoast';
import { wechat } from "../../wailsjs/go/models";
import WxapkgItem = wechat.WxapkgItem;

const DEFAULT_LIFE = 3000;

export function useAppToast() {
    const toast = useToast();

    const show = (
        severity: 'info' | 'success' | 'warn' | 'error',
        title: string,
        message: string
    ) => {
        toast.add({
            severity,
            summary: title,
            detail: message,
            life: DEFAULT_LIFE
        });
    };

    return {
        info: (title: string, message: string) => show('info', title, message),
        success: (title: string, message: string) => show('success', title, message),
        warning: (title: string, message: string) => show('warn', title, message),
        error: (title: string, message: string) => show('error', title, message),
    };
}


export function formatSize(bytes: number | string | undefined): string {
    if (bytes == null || bytes === '') return '—';
    const num = typeof bytes === 'string' ? parseFloat(bytes) : bytes;
    if (isNaN(num)) return String(bytes);

    const units = ['B', 'KB', 'MB', 'GB'];
    let i = 0;
    let value = num;
    while (value >= 1024 && i < units.length - 1) {
        value /= 1024;
        i++;
    }
    return `${value.toFixed(i ? 1 : 0)} ${units[i]}`;
};


export function formatTime(timestamp: number, verbose: boolean): string {
    if (!timestamp) return '—';

    const date = new Date(timestamp * 1000);
    if (isNaN(date.getTime())) return 'Invalid';

    // --- 1. 格式化绝对时间：YYYY-MM-DD HH:mm:ss ---
    const year = date.getFullYear();
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const hours = String(date.getHours()).padStart(2, '0');
    const minutes = String(date.getMinutes()).padStart(2, '0');
    const seconds = String(date.getSeconds()).padStart(2, '0');
    const absolute = `${year}/${month}/${day} ${hours}:${minutes}:${seconds}`;

    // --- 2. 计算相对时间 ---
    const now = Date.now();
    const diffMs = now - date.getTime();

    // 未来时间
    if (diffMs < 0) {
        return `${absolute}（未来）`;
    }

    const secondsDiff = Math.floor(diffMs / 1000);
    const minutesDiff = Math.floor(secondsDiff / 60);
    const hoursDiff = Math.floor(minutesDiff / 60);
    const daysDiff = Math.floor(hoursDiff / 24);

    let relative: string;
    if (secondsDiff < 60) {
        relative = '刚刚';
    } else if (minutesDiff < 60) {
        relative = `${minutesDiff}分钟前`;
    } else if (hoursDiff < 24) {
        relative = `${hoursDiff}小时前`;
    } else {
        relative = `${daysDiff}天前`;
    }

    return verbose ? `${absolute} ${relative}` : absolute;
}

export enum UnpackStatusType {
    Running = "running",
    Finished = "finished",
    Error = "error",
    Cancelled = "cancelled",
}

export interface UnpackButtonProps {
    icon: string;
    label: string;
    disabled: boolean;
    class: string;
    action?: () => void;
}

export function getUnpackButtonProps(item: WxapkgItem, unpackAction: () => void, openResultAction: () => void): UnpackButtonProps {
    if (item.UnpackStatus === UnpackStatusType.Running) {
        return {
            icon: 'pi pi-spin pi-spinner',
            label: '解包中',
            disabled: true,
            class: 'text-blue-500'
        };
    } else if (item.UnpackStatus === UnpackStatusType.Finished) {
        return {
            icon: 'pi pi-folder-open',
            label: '打开目录',
            disabled: false,
            class: 'text-green-500',
            action: openResultAction
        };
    } else if (item.UnpackStatus === UnpackStatusType.Error) {
        return {
            icon: 'pi pi-times',
            label: '失败',
            disabled: true,
            class: 'text-red-500'
        };
    } else if (item.UnpackStatus === UnpackStatusType.Cancelled) {
        return {
            icon: 'pi pi-ban',
            label: '重新解包',
            disabled: false,
            class: 'text-primary',
            action: unpackAction
        };
    } else {
        return {
            icon: 'pi pi-box',
            label: '解包',
            disabled: false,
            class: 'text-primary',
            action: unpackAction
        };
    }
}

export function formatProgress(progress: number): string {
    return `${Math.round(progress)}%`;
}

export function calculateStats(items: WxapkgItem[]) {
    return {
        total: items.length,
        unpacking: items.filter(i => i.UnpackStatus === UnpackStatusType.Running).length,
        completed: items.filter(i => i.UnpackStatus === UnpackStatusType.Finished).length,
        errors: items.filter(i => i.UnpackStatus === UnpackStatusType.Error).length
    };
}
