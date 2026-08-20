/// <reference types="vite/client" />

declare module '*.vue' {
    import type {DefineComponent} from 'vue'
    const component: DefineComponent<{}, {}, any>
    export default component
}

interface WailsBindings {
    'github.com_wux1an_wxapkg': {
        AppService: {
            ClipboardSetText(text: string): Promise<void>;
            CancelUnpack(uuid: string): Promise<boolean>;
            ComputeSavePath(outputDir: string, wxapkgPath: string): Promise<string>;
            GetDefaultPaths(): Promise<import('../wailsjs/go/models').wechat.PathScanResult>;
            GetWxapkgItem(uuid: string): Promise<import('../wailsjs/go/models').wechat.WxapkgItem | null>;
            Github(): Promise<string>;
            OpenDirectoryDialog(title: string, root: string): Promise<string>;
            OpenFileDialog(title: string, root: string, filters: Array<import('../wailsjs/go/models').main.FileFilter>): Promise<string>;
            OpenPath(path: string): Promise<void>;
            OpenUrl(url: string): Promise<void>;
            ScanWxapkgItem(path: string, scan: boolean): Promise<import('../wailsjs/go/models').wechat.WxapkgItem[]>;
            UnpackWxapkgItem(item: import('../wailsjs/go/models').wechat.WxapkgItem, options: import('../wailsjs/go/models').wechat.UnpackOptions): Promise<void>;
            Version(): Promise<string>;
        };
    };
}

interface Window {
    go: WailsBindings;
    runtime: {
        EventsOn(eventName: string, callback: (data: unknown) => void): void;
        EventsOff(eventName: string): void;
        EventsEmit(eventName: string, data?: unknown): void;
    };
}
