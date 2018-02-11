program=smzdm

all:
	go build -o smzdm main.go

clean:
	rm -f smzdm