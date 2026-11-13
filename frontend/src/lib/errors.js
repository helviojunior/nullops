// Server messages are English; translate the known ones (EN stays as-is).
const PT = {
  "Invalid path": "Caminho inválido",
  "Invalid name": "Nome inválido",
  "Not found": "Não encontrado",
  "Not a file": "Não é um arquivo",
  "Not a directory": "Não é uma pasta",
  "Already exists": "Já existe",
  "Target already exists": "O destino já existe",
  "Destination is not a directory": "O destino não é uma pasta",
  "Read-only mode": "Modo somente leitura",
  "File too large to edit": "Arquivo grande demais para editar",
  "File is not valid UTF-8 text": "O arquivo não é texto UTF-8 válido",
  "Cannot copy or move a folder into itself": "Não é possível copiar ou mover uma pasta para dentro dela mesma",
  "Unsafe path in archive": "Caminho inseguro no arquivo compactado",
  "Unsupported archive format": "Formato de arquivo compactado não suportado",
  "Archive already exists": "O arquivo compactado já existe",
  "File extension not allowed": "Extensão de arquivo não permitida",
  "Refusing to delete the root": "Não é permitido excluir a raiz",
  "Invalid mode": "Modo inválido",
  "Operation failed": "A operação falhou",
  "Decryption failed": "Falha ao decifrar",
  "Not authenticated": "Não autenticado",
  "No session": "Sem sessão",
};

export function errorText(err, lang) {
  const msg = (err && err.message) || String(err || "");
  if (lang === "pt-br" && PT[msg]) return PT[msg];
  return msg;
}
