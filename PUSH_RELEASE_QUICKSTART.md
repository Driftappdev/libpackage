# Push/Release Quick Commands

Repository module path: `github.com/platformcore/libpackage`

## 1) Push all current code
```powershell
git add -A
git commit -m "chore: update libpackage"
git push origin main
```

## 2) Release one module (create + push one tag)
```powershell
powershell -ExecutionPolicy Bypass -File scripts/release_one_module.ps1 -ModulePath "platform/eventbus" -Version "v0.2.0"
```

## 3) Release all modules from matrix
```powershell
powershell -ExecutionPolicy Bypass -File scripts/release_all_from_matrix.ps1 -MatrixFile "PACKAGE_RELEASE_MATRIX.md"
```

## 4) Install module separately
```bash
go get github.com/platformcore/libpackage/platform/eventbus@v0.2.0
```
