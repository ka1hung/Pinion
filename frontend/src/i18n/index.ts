// Merges every namespace dictionary. Add new namespaces here.

import * as common from "./common";
import * as toolbar from "./toolbar";
import * as welcome from "./welcome";
import * as statusbar from "./statusbar";
import * as prompt from "./prompt";
import * as sessionEditor from "./sessionEditor";
import * as importDialog from "./importDialog";
import * as quickConnect from "./quickConnect";
import * as vault from "./vault";
import * as sftp from "./sftp";
import * as perm from "./perm";
import * as transfers from "./transfers";
import * as settings from "./settings";
import * as editor from "./editor";
import * as sidebar from "./sidebar";
import * as terminal from "./terminal";
import * as servers from "./servers";
import * as tunnels from "./tunnels";
import * as workspace from "./workspace";
import * as multiExec from "./multiExec";
import * as monitor from "./monitor";

export const zh = {
  ...common.zh,
  ...toolbar.zh,
  ...welcome.zh,
  ...statusbar.zh,
  ...prompt.zh,
  ...sessionEditor.zh,
  ...importDialog.zh,
  ...quickConnect.zh,
  ...vault.zh,
  ...sftp.zh,
  ...perm.zh,
  ...transfers.zh,
  ...settings.zh,
  ...editor.zh,
  ...sidebar.zh,
  ...terminal.zh,
  ...servers.zh,
  ...tunnels.zh,
  ...workspace.zh,
  ...multiExec.zh,
  ...monitor.zh,
};

export const en: Record<keyof typeof zh, string> = {
  ...common.en,
  ...toolbar.en,
  ...welcome.en,
  ...statusbar.en,
  ...prompt.en,
  ...sessionEditor.en,
  ...importDialog.en,
  ...quickConnect.en,
  ...vault.en,
  ...sftp.en,
  ...perm.en,
  ...transfers.en,
  ...settings.en,
  ...editor.en,
  ...sidebar.en,
  ...terminal.en,
  ...servers.en,
  ...tunnels.en,
  ...workspace.en,
  ...multiExec.en,
  ...monitor.en,
};
