import { wechat } from "../../wailsjs/go/models";
import {UnpackStatusType} from "./util";
import WxapkgItem = wechat.WxapkgItem;

export class ScanPathItem {
    path: string;
    scan: boolean;

    constructor(path: string, scan: boolean) {
        this.path = path;
        this.scan = scan;
    }
}

export const EventUnpackProgress = "unpack:progress-changed"
