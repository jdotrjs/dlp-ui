export namespace binaries {
	
	export class BinaryStatus {
	    name: string;
	    resolved: string;
	    source: string;
	    ok: boolean;
	    version: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new BinaryStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.resolved = source["resolved"];
	        this.source = source["source"];
	        this.ok = source["ok"];
	        this.version = source["version"];
	        this.error = source["error"];
	    }
	}

}

export namespace config {
	
	export class Config {
	    ytdlpPath: string;
	    ffmpegPath: string;
	    denoPath: string;
	    downloadDir: string;
	    dbPath: string;
	    outputTemplate: string;
	    restrictFilenames: boolean;
	    playlistMode: string;
	    playlistTemplate: string;
	    maxConcurrent: number;
	    skipYtdlpVersionCache: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ytdlpPath = source["ytdlpPath"];
	        this.ffmpegPath = source["ffmpegPath"];
	        this.denoPath = source["denoPath"];
	        this.downloadDir = source["downloadDir"];
	        this.dbPath = source["dbPath"];
	        this.outputTemplate = source["outputTemplate"];
	        this.restrictFilenames = source["restrictFilenames"];
	        this.playlistMode = source["playlistMode"];
	        this.playlistTemplate = source["playlistTemplate"];
	        this.maxConcurrent = source["maxConcurrent"];
	        this.skipYtdlpVersionCache = source["skipYtdlpVersionCache"];
	    }
	}

}

export namespace db {
	
	export class Content {
	    id: number;
	    source: string;
	    sourceId: string;
	    title: string;
	    uploader: string;
	    duration: number;
	    uploadDate: string;
	    thumbnailUrl: string;
	    ext: string;
	    filesize: number;
	    sourceUrl: string;
	    filepath: string;
	    status: string;
	    error: string;
	    playlistId: number;
	    playlistIndex: number;
	    downloadedAt: number;
	    playlistTitle: string;
	
	    static createFrom(source: any = {}) {
	        return new Content(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source = source["source"];
	        this.sourceId = source["sourceId"];
	        this.title = source["title"];
	        this.uploader = source["uploader"];
	        this.duration = source["duration"];
	        this.uploadDate = source["uploadDate"];
	        this.thumbnailUrl = source["thumbnailUrl"];
	        this.ext = source["ext"];
	        this.filesize = source["filesize"];
	        this.sourceUrl = source["sourceUrl"];
	        this.filepath = source["filepath"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.playlistId = source["playlistId"];
	        this.playlistIndex = source["playlistIndex"];
	        this.downloadedAt = source["downloadedAt"];
	        this.playlistTitle = source["playlistTitle"];
	    }
	}
	export class ListOptions {
	    sortBy: string;
	    sortDir: string;
	    limit: number;
	    offset: number;
	    source: string;
	    playlistId: number;
	
	    static createFrom(source: any = {}) {
	        return new ListOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sortBy = source["sortBy"];
	        this.sortDir = source["sortDir"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	        this.source = source["source"];
	        this.playlistId = source["playlistId"];
	    }
	}
	export class ListResult {
	    items: Content[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new ListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], Content);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace download {
	
	export class DownloadRequest {
	    url: string;
	    isPlaylist: boolean;
	    selectedIndices: number[];
	    formatId: string;
	    audioOnly: boolean;
	    outputTemplate: string;
	    playlistModeOverride: string;
	    downloadDir: string;
	    playlistTitle: string;
	    playlistSourceId: string;
	
	    static createFrom(source: any = {}) {
	        return new DownloadRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.isPlaylist = source["isPlaylist"];
	        this.selectedIndices = source["selectedIndices"];
	        this.formatId = source["formatId"];
	        this.audioOnly = source["audioOnly"];
	        this.outputTemplate = source["outputTemplate"];
	        this.playlistModeOverride = source["playlistModeOverride"];
	        this.downloadDir = source["downloadDir"];
	        this.playlistTitle = source["playlistTitle"];
	        this.playlistSourceId = source["playlistSourceId"];
	    }
	}

}

export namespace updater {
	
	export class Info {
	    versionName: string;
	    currentVersion: string;
	    latestVersion: string;
	    updateAvailable: boolean;
	    lastCheckedAt: number;
	    releaseUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionName = source["versionName"];
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.updateAvailable = source["updateAvailable"];
	        this.lastCheckedAt = source["lastCheckedAt"];
	        this.releaseUrl = source["releaseUrl"];
	    }
	}

}

export namespace ytdlp {
	
	export class Format {
	    formatId: string;
	    ext: string;
	    resolution: string;
	    fps: number;
	    filesize: number;
	    vcodec: string;
	    acodec: string;
	    note: string;
	
	    static createFrom(source: any = {}) {
	        return new Format(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.formatId = source["formatId"];
	        this.ext = source["ext"];
	        this.resolution = source["resolution"];
	        this.fps = source["fps"];
	        this.filesize = source["filesize"];
	        this.vcodec = source["vcodec"];
	        this.acodec = source["acodec"];
	        this.note = source["note"];
	    }
	}
	export class PreviewEntry {
	    source: string;
	    sourceId: string;
	    title: string;
	    uploader: string;
	    thumbnail: string;
	    duration: number;
	    index: number;
	    url: string;
	
	    static createFrom(source: any = {}) {
	        return new PreviewEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.sourceId = source["sourceId"];
	        this.title = source["title"];
	        this.uploader = source["uploader"];
	        this.thumbnail = source["thumbnail"];
	        this.duration = source["duration"];
	        this.index = source["index"];
	        this.url = source["url"];
	    }
	}
	export class Preview {
	    url: string;
	    isPlaylist: boolean;
	    source: string;
	    title: string;
	    uploader: string;
	    thumbnail: string;
	    duration: number;
	    formats: Format[];
	    entries: PreviewEntry[];
	    playlistId: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.isPlaylist = source["isPlaylist"];
	        this.source = source["source"];
	        this.title = source["title"];
	        this.uploader = source["uploader"];
	        this.thumbnail = source["thumbnail"];
	        this.duration = source["duration"];
	        this.formats = this.convertValues(source["formats"], Format);
	        this.entries = this.convertValues(source["entries"], PreviewEntry);
	        this.playlistId = source["playlistId"];
	        this.count = source["count"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

