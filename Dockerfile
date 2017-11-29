FROM ubuntu:14.04.3
ADD . /code
WORKDIR /code
CMD ./external-lb
