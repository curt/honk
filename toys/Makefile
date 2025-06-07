
PROGS=autobonker gettoken newstrigger saytheday sprayandpray youvegothonks

all: $(PROGS)

clean:
	rm -f $(PROGS)

autobonker: autobonker.go
	go build autobonker.go

gettoken: gettoken.go
	go build gettoken.go fetch.go

saytheday: saytheday.go
	go build saytheday.go

newstrigger: newstrigger.go
	go build newstrigger.go

sprayandpray: sprayandpray.go
	go build sprayandpray.go

youvegothonks: youvegothonks.go
	go build youvegothonks.go
