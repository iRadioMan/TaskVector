.PHONY: dev build
dev:
	cd web && npm install
	@powershell -NoProfile -Command "Start-Process -WindowStyle Hidden -FilePath 'go' -ArgumentList 'run ./cmd/taskvector' -WorkingDirectory (Get-Location) -RedirectStandardOutput 'taskvector-dev.stdout.log' -RedirectStandardError 'taskvector-dev.log'"
	cd web && npm run dev
build:
	cd web && npm run build
	powershell -NoProfile -Command "New-Item -ItemType Directory -Force cmd/taskvector/web | Out-Null; Copy-Item web/dist cmd/taskvector/web -Recurse -Force"
	go build -o taskvector.exe ./cmd/taskvector
