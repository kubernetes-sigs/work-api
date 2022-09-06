work_creation.py creates an example work 'n' number of times. The correct usage is: python3 work_creation.py n where n is the number of works to be created.


../fleet/hack/tools/bin/goimports-latest -local sigs.k8s.io/work-api -w $(go list -f {{.Dir}} ./...)
../fleet/hack/tools/bin/staticcheck ./...