/// <reference types="vite/client" />

declare module '*.vue' {
    import type {DefineComponent} from 'vue'
    const component: DefineComponent<{}, {}, any>
    export default component
}

interface WailsBindings {
    github.com_wux1an_wxapkg: {
        AppService: {
            ClipboardSetText(text: string): Promise<boolean>;
            CancelUnpack(uuid: string): Promise<boolean>;
            ComputeSavePath(outputDir: string, wxapkgPath: string): Promise<string>;
            GetDefaultPaths(): Promise<string[]>;
            GetWxapkgItem(uuid: string): Promise<import('./bindings/github.com/wux1an/wxapkg/wechat').WxapkgItem | null>;
            Github(): Promise<string>;
            OpenDirectoryDialog(title: string, root: string): Promise<string>;
            OpenFileDialog(title: string, root: string, filters: FileFilter[]): Promise<string>;
            OpenPath(path: string): Promise<void>;
            OpenUrl(url: string): Promise<void>;
            ScanWxapkgItem(path: string, scan: boolean): Promise<import('./bindings/github.com/wux1an/wxapkg/wechat').WxapkgItem[]>;
            UnpackWxapkgItem(item: import('./bindings/github.com/wux1an/wxapkg/wechat').WxapkgItem, options: import('./bindings/github.com/wux1an/wxapkg/wechat').UnpackOptions): Promise<void>;
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
