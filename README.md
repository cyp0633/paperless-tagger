# paperless-tagger

An LLM-based simple tagger and OCR enhancement for paperless-ngx.

## Features

- automatically OCR new documents into markdown, and extract related metadata from the document
- zh-CN and en-US web interface
- OpenAI Completion API for separate OCR and extraction configuration, with extractor system prompt customization
- no RAG or chat feature, low footprint

I personally recommend Qwen3.6-Flash for OCR and extraction. It's both cheap and accurate.

## Thanks

paperless-ai and paperless-gpt for inspiration. Many features are heavily borrowed from them.