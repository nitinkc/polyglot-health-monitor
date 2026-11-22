from fastapi import FastAPI
from fastapi.responses import PlainTextResponse

app = FastAPI()

@app.get("/health", response_class=PlainTextResponse)
def health():
    return "OK"
