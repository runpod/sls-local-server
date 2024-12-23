FROM python:3.11-slim

WORKDIR /app

COPY . .

RUN pip install -r requirements.txt

RUN chmod +x /app/script.sh

CMD ["sh", "/app/script.sh"]