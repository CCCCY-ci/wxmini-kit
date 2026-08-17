import { WxapkgItem } from "../../bindings/github.com/wux1an/wxapkg/wechat";
import {UnpackStatusType} from "./util";

export class ScanPathItem {
    path: string;
    scan: boolean;

    constructor(path: string, scan: boolean) {
        this.path = path;
        this.scan = scan;
    }
}

export const EventUnpackProgress = "unpack:progress-changed"