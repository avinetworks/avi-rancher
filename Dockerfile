FROM ubuntu:16.04
ADD . /code
WORKDIR /code
CMD ./avi-rancher
