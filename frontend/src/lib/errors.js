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
  "Invalid code": "Código inválido",
  "Login expired": "O login expirou",
  "Too many attempts": "Muitas tentativas",
  "Start the MFA setup again": "Inicie a configuração do MFA novamente",
  "MFA is already enabled": "O MFA já está ativado",
  "File too large": "Arquivo grande demais",
  "Upload out of order": "Envio fora de ordem",
  "The file was changed on disk since it was opened": "O arquivo foi alterado no disco desde que foi aberto",
  "Download failed": "O download falhou",
  "Truncated download": "Download incompleto",
  "Nothing selected": "Nada selecionado",
  "Pad not found": "Pad não encontrado",
  "A pad with this name already exists": "Já existe um pad com este nome",
  "Invalid pad name (allowed: letters, digits, . _ -)": "Nome de pad inválido (permitido: letras, números, . _ -)",
  "Pad is too large": "O pad é grande demais",
};

export function errorText(err, lang) {
  const msg = (err && err.message) || String(err || "");
  if (lang === "pt-br" && PT[msg]) return PT[msg];
  return msg;
}
