// "perm" strings. Keys are prefixed "perm." ; en must list exactly the same keys.
export const zh = {
  "perm.title": "變更權限",
  "perm.apply": "套用",
  "perm.owner": "擁有者 (Owner)",
  "perm.group": "群組 (Group)",
  "perm.others": "其他人 (Others)",
  "perm.read": "讀取 (r)",
  "perm.write": "寫入 (w)",
  "perm.exec": "執行 (x)",
  "perm.octal": "八進位",
  "perm.ids": "（uid {uid} / gid {gid}）",
} as const;

export const en: Record<keyof typeof zh, string> = {
  "perm.title": "Change permissions",
  "perm.apply": "Apply",
  "perm.owner": "Owner",
  "perm.group": "Group",
  "perm.others": "Others",
  "perm.read": "Read (r)",
  "perm.write": "Write (w)",
  "perm.exec": "Execute (x)",
  "perm.octal": "Octal",
  "perm.ids": "(uid {uid} / gid {gid})",
};
