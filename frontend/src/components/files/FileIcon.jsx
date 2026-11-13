import {
  Folder,
  FileText,
  FileCode,
  FileImage,
  FileArchive,
  FileAudio,
  FileVideo,
  FileSpreadsheet,
  File as FileGeneric,
  Link2,
} from "lucide-react";

const MAP = [
  [/\.(png|jpe?g|gif|bmp|svg|webp|ico|tiff?)$/i, FileImage, "text-pink-500"],
  [/\.(zip|tar|gz|tgz|bz2|xz|7z|rar)$/i, FileArchive, "text-amber-500"],
  [/\.(mp3|wav|flac|ogg|m4a)$/i, FileAudio, "text-purple-500"],
  [/\.(mp4|mkv|avi|mov|webm)$/i, FileVideo, "text-purple-500"],
  [/\.(csv|tsv|xlsx?|ods)$/i, FileSpreadsheet, "text-emerald-500"],
  [/\.(js|jsx|ts|tsx|go|py|rb|php|java|c|h|cpp|rs|sh|json|ya?ml|toml|xml|html?|css|sql)$/i, FileCode, "text-sky-500"],
  [/\.(txt|md|log|ini|conf|cfg|pdf|docx?|odt|rtf)$/i, FileText, "text-muted-foreground"],
];

export default function FileIcon({ entry, className = "h-4 w-4" }) {
  if (entry.is_dir) return <Folder className={`${className} text-amber-400`} fill="currentColor" fillOpacity={0.25} />;
  for (const [re, Icon, color] of MAP) {
    if (re.test(entry.name)) return <Icon className={`${className} ${color}`} />;
  }
  if (entry.is_link) return <Link2 className={`${className} text-muted-foreground`} />;
  return <FileGeneric className={`${className} text-muted-foreground`} />;
}
