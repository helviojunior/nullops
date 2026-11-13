# Pull Request

## 📋 Description

<!-- Descreva o que esta PR faz e por que é necessária -->

### Type of Change
<!-- Marque o tipo de mudança com [x] -->

- [ ] 🐛 Bug fix (correção que resolve um problema)
- [ ] ✨ New feature (funcionalidade que adiciona algo novo)
- [ ] 💥 Breaking change (mudança que quebra compatibilidade)
- [ ] 📚 Documentation update (atualização de documentação)
- [ ] 🔧 Refactoring (mudança de código que não adiciona funcionalidade nem corrige bug)
- [ ] ⚡ Performance improvement (melhoria de performance)
- [ ] 🔒 Security fix (correção de vulnerabilidade)
- [ ] 🧪 Test (adição ou correção de testes)
- [ ] 🔨 Build/CI (mudanças no processo de build ou CI)

## 🎯 Motivation and Context

<!-- Por que essa mudança é necessária? Que problema resolve? -->
<!-- Se resolve uma issue, referencie com: Fixes #123, Closes #456 -->

**Issue(s) relacionada(s):**

**Contexto:**

## 🔍 Changes Made

<!-- Liste as principais mudanças feitas -->

### Code Changes:
-
-

### Areas Affected:
- [ ] Back-end Go (`pkg/`, `internal/`)
- [ ] CLI commands (`cmd/`: `web`, `create-tls`, `add-user`, `version`)
- [ ] HTTP API / auth / sessions
- [ ] Payload encryption / TLS
- [ ] Front-end (`frontend/`: React + Vite)
- [ ] Asset bundle (`tools/assetpack`, `pkg/assets`)
- [ ] Build (`Makefile`, `frontend/Dockerfile`) / CI (`.github/`)

### Documentation Changes:
-

## 🧪 Testing

<!-- Descreva como você testou suas mudanças -->

### Tests Added/Modified:
- [ ] Go unit tests adicionados/modificados
- [ ] Front-end unit tests (vitest) adicionados/modificados
- [ ] Manual testing realizado

### Testing Checklist:
- [ ] `make assets` (front-end + bundle) executado
- [ ] `make test` (Go) passando
- [ ] `make test-frontend` passando
- [ ] `make local` compila e `./build/nullops_<os>_<arch> --help` executa
- [ ] Testei a interface web no navegador (`nullops web`), se aplicável
- [ ] Testei com e sem `--no-tls`, se aplicável

## 🔒 Security

<!-- Considerações de segurança -->

- [ ] Esta mudança não introduz vulnerabilidades de segurança
- [ ] Não contém hardcoded secrets, chaves TLS ou credenciais
- [ ] Caminhos de arquivo validados (sem path traversal fora da raiz servida)
- [ ] Autenticação/autorização verificadas nos endpoints novos ou alterados
- [ ] Input validation e output sanitization adequados (se aplicável)

## 📊 Performance

- [ ] Esta mudança não degrada a performance
- [ ] Uso de memória verificado com arquivos/diretórios grandes (se aplicável)
- [ ] Tamanho do bundle do front-end não aumentou significativamente

## 🔗 Dependencies

- [ ] Não adiciona novas dependências
- [ ] Se adiciona dependências, justificativa fornecida
- [ ] `go.mod`/`go.sum` e `frontend/package-lock.json` atualizados corretamente

## 📝 Documentation

- [ ] README.md atualizado (se necessário)
- [ ] Comentários adicionados para código complexo
- [ ] Help text atualizado para novos comandos/flags

## ✅ Pre-Submission Checklist

### Code Quality:
- [ ] Código formatado com `make fmt` (`gofmt -s -w .`)
- [ ] `go vet ./...` passou sem erros
- [ ] Não há comentários TODO desnecessários

### Git:
- [ ] Commit messages são claros e descritivos
- [ ] Branch baseada na versão mais recente do main
- [ ] Não há conflitos de merge
- [ ] Nenhum arquivo gerado commitado (`frontend/dist`, `pkg/assets/bundle.bin`, `build/`)

### CI/CD:
- [ ] GitHub Actions passarão (verificado localmente)
- [ ] Não há secrets hardcoded

## 🎥 Screenshots/Demos

<!-- Para mudanças de interface, adicione screenshots ou demos -->

## 📋 Additional Notes

### Breaking Changes:
<!-- Se há breaking changes (flags, API, formato do estado em disco), documente aqui -->

### Migration Guide:
<!-- Se necessário, forneça guia de migração -->

---

## 👀 Reviewer Checklist

### Code Review:
- [ ] Código segue os padrões do projeto
- [ ] Error handling adequado
- [ ] Performance não foi degradada
- [ ] Segurança foi considerada (auth, sessões, path traversal, TLS)

### Testing Review:
- [ ] Testes cobrem cenários importantes
- [ ] Testes são estáveis e determinísticos

---

**Thank you for contributing to nullops! 🗂️**
