no code edit needed

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bootstrap main.go
zip -r bootstrap.zip bootstrap .env
```


- amazon linux 2023
- x86_64
- change default execution role
- use existing role
- dynamo db full access

- put the bootstrap.zip in the lambda function
- change the handler to main.handler
- configuration > function url > enable
                                - cors enabled
                                    -  allow origins: *
                                    -  allow headers: Content-Type
                                    -  allow methods: *

done


