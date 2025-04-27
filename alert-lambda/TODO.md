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
                                    - auth type: none
                                    -  allow origins: *
                                    -  allow headers: Content-Type
                                    -  allow methods: *

done


```sh

curl -X POST -H "Content-Type: application/json" -d '{"temperature": 105}' https://gzdavjn3b3cgqvqjcbtylasx6u0ffwie.lambda-url.ap-south-1.on.aws/
```
