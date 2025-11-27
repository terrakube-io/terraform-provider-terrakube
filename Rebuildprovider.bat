cd  C:\dump\github\terraform-provider-terrakube
del terraform-provider-terrakube_v1.0.0.exe

go install .
pause

go generate ./...
pause

set GOOS=windows
set GOARCH=amd64
go build -o terraform-provider-terrakube_v1.0.0.exe

del C:\dump\github\service-itops-terrakube\config\terraform.d\plugins\registry.terraform.io\terrakube-io\terrakube\1.0.0\windows_amd64\terraform-provider-terrakube_v1.0.0.exe

robocopy "." "C:\dump\github\service-itops-terrakube\config\terraform.d\plugins\registry.terraform.io\terrakube-io\terrakube\1.0.0\windows_amd64" terraform-provider-terrakube_v1.0.0.exe

rmdir "C:\dump\github\service-itops-terrakube\config\.terraform" /s /q
del C:\dump\github\service-itops-terrakube\config\.terraform.lock.hcl

cd C:\dump\github\service-itops-terrakube\config
terraform init 
pause
