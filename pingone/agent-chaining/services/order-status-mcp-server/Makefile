VENV := .venv
PY   := $(VENV)/bin/python

.PHONY: venv run clean

venv:
	python3 -m venv $(VENV)
	$(PY) -m pip install --quiet --upgrade pip
	$(PY) -m pip install --quiet -r requirements.txt

run: venv
	$(PY) -m uvicorn main:app --host 0.0.0.0 --port 8082

clean:
	rm -rf $(VENV)
