// client.ts re-exports the Wails-generated bound methods behind a stable
// import path, so view code imports from "../api/client" rather than reaching
// into the generated wailsjs tree directly. This keeps the binding location an
// implementation detail and gives one place to wrap/adapt calls later.
import {
  GetConfig,
  SaveConfig,
  ResolveBinaries,
  PickDirectory,
  PickFile,
  ListContent,
  GetContent,
  DeleteContent,
  OpenFile,
  OpenInFolder,
  GetThumbnailDataURL,
  FetchMetadata,
  StartDownload,
  CancelDownload,
  IsFirstRun,
  ConfigPath,
  VendorDir,
  InstallDependency,
} from '../../wailsjs/go/main/App';

// The Go-side namespace is now `db` (it owns more than the library catalogue),
// but existing frontend code imports the rows as `library.Content` etc. Alias
// the namespace at this seam so the rename doesn't ripple through every view.
export { config, binaries, db as library, ytdlp, download } from '../../wailsjs/go/models';

export {
  GetConfig,
  SaveConfig,
  ResolveBinaries,
  PickDirectory,
  PickFile,
  ListContent,
  GetContent,
  DeleteContent,
  OpenFile,
  OpenInFolder,
  GetThumbnailDataURL,
  FetchMetadata,
  StartDownload,
  CancelDownload,
  IsFirstRun,
  ConfigPath,
  VendorDir,
  InstallDependency,
};
