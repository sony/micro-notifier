all:
	@echo "'make run' to run test processes"

run: ../main/notifier testapp/testapp testclient/testclient
	./runservers.sh

../main/notifier :
	(cd ../main; go build notifier.go)

testapp/testapp :
	(cd testapp; go build)

testclient/testclient :
	(cd testclient; go build)
