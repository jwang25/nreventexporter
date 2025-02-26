import time
import socket

SERVER_IP = "127.0.0.1"
SERVER_PORT = 8125

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)

count = 0
countmax = 100
counter = 0

while True:
    count = 0
    while count < countmax:
        message = f"fake.metric.{count}:{countmax - count}|g|#env:prod,app:web,host:server1,eventType:jw_otel_test"
        sock.sendto(message.encode(), (SERVER_IP, SERVER_PORT))
        print(f"Sent: {message}")

        count += 1
        time.sleep(0.1)
    message = f"fake.counter:{counter}|c|#env:prod,app:web,host:server1,eventType:jw_otel_test"
    sock.sendto(message.encode(), (SERVER_IP, SERVER_PORT))
    print(f"Sent: {message}")
    counter += 10
    time.sleep(30)
