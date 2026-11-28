#!/usr/bin/env python3
"""
SWE-bench 5-instance run using the new multi-agent Nebula harness.
Uses real test patches from HuggingFace princeton-nlp/SWE-bench.
Protocol: git reset --hard → apply test_patch → nebula do → verify
"""

import json, os, subprocess, sys, tempfile, time

NEBULA_BIN    = os.path.expanduser("~/.local/bin/nebula")
NEBULA_CONFIG = os.path.expanduser("~/.config/nebula/config.toml")
RESULTS_FILE  = "/tmp/swe_harness_results.json"
LOG_FILE      = "/tmp/swe_harness_run.log"
DATA_FILE     = "/tmp/swe5_data.json"
INSTANCE_TIMEOUT = 1500  # orchestrator budget; outer cap adds a margin below
ACTIVATE      = "source /opt/miniconda3/etc/profile.d/conda.sh && conda activate testbed"

IMAGES = {
    "astropy__astropy-14309":        "swebench/sweb.eval.x86_64.astropy_1776_astropy-14309:latest",
    "django__django-10097":          "swebench/sweb.eval.x86_64.django_1776_django-10097:latest",
    "matplotlib__matplotlib-13989":  "swebench/sweb.eval.x86_64.matplotlib_1776_matplotlib-13989:latest",
    "pallets__flask-5014":           "swebench/sweb.eval.x86_64.pallets_1776_flask-5014:latest",
    "psf__requests-1142":            "swebench/sweb.eval.x86_64.psf_1776_requests-1142:latest",
}

def log(msg):
    ts = time.strftime("%H:%M:%S")
    line = f"[{ts}] {msg}"
    print(line, flush=True)
    with open(LOG_FILE, "a") as f:
        f.write(line + "\n")

def run(cmd, timeout=60):
    r = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
    return r.returncode, r.stdout + r.stderr

def write_to_container(container, content, dest_path):
    with tempfile.NamedTemporaryFile(mode='w', suffix='.tmp', delete=False) as f:
        f.write(content)
        tmp = f.name
    code, out = run(f"docker cp {tmp} {container}:{dest_path}", timeout=10)
    os.unlink(tmp)
    return code == 0, out

def dexec(container, cmd, timeout=120):
    with tempfile.NamedTemporaryFile(mode='w', suffix='.sh', delete=False) as f:
        f.write("#!/usr/bin/env bash\n")
        f.write(cmd + "\n")
        tmp = f.name
    run(f"docker cp {tmp} {container}:/tmp/_dexec.sh", timeout=10)
    os.unlink(tmp)
    return run(f"docker exec {container} bash /tmp/_dexec.sh", timeout=timeout)

def get_keys():
    keys = {}
    for var in ["NEBULA_NVIDIA_KEY", "NEBULA_GROQ_KEY", "NEBULA_MISTRAL_KEY"]:
        v = os.environ.get(var, "")
        if not v and os.path.exists("/tmp/nebula_keys.env"):
            for line in open("/tmp/nebula_keys.env"):
                line = line.strip()
                if line.startswith(var + "="):
                    v = line.split("=", 1)[1]
                    break
        if v:
            keys[var] = v
    return keys

def run_instance(iid, data, keys):
    image       = IMAGES[iid]
    prob        = data["problem_statement"]
    patch       = data["test_patch"]
    base_commit = data.get("base_commit", "HEAD")
    fail_tests  = json.loads(data["FAIL_TO_PASS"]) if isinstance(data["FAIL_TO_PASS"], str) else data["FAIL_TO_PASS"]

    log(f"\n{'='*60}")
    log(f"INSTANCE: {iid}")
    log(f"TESTS:    {fail_tests[:2]}")

    # Detect test format: Django uses unittest style "method (module.Class)"
    is_django = iid.startswith("django__")
    if is_django:
        # Convert "test_foo (app.tests.TestClass)" -> "app.tests"
        test_modules = list(dict.fromkeys(
            t.split("(")[1].rstrip(")").rsplit(".", 1)[0]
            for t in fail_tests[:3] if "(" in t
        ))
        t_args = " ".join(test_modules)
        test_cmd = (f'set -o pipefail; {ACTIVATE} && cd /testbed && '
                    f'git apply --whitespace=fix /tmp/swe_test.patch 2>/dev/null || true && '
                    f'python tests/runtests.py --verbosity=0 {t_args} 2>&1 | tail -25')
    else:
        t_args = " ".join(f'"{t}"' for t in fail_tests[:3])
        # Must re-apply test_patch before verifying: nebula may have reset the repo
        test_cmd = f'set -o pipefail; {ACTIVATE} && cd /testbed && git apply --whitespace=fix /tmp/swe_test.patch 2>/dev/null || true && python -m pytest {t_args} -x --tb=short -q 2>&1 | tail -25'

    nv = keys.get("NEBULA_NVIDIA_KEY", "")
    gq = keys.get("NEBULA_GROQ_KEY", "")
    ms = keys.get("NEBULA_MISTRAL_KEY", "")

    container = f"neb-{iid.replace('__','-').replace('_','-')[:30]}-{int(time.time())%10000}"

    try:
        docker_run = (
            f"docker run -d --name {container} "
            f"--memory 4g --memory-swap 4g "
            f"-e NEBULA_NVIDIA_KEY={nv} "
            f"-e NEBULA_GROQ_KEY={gq} "
            f"-e NEBULA_MISTRAL_KEY={ms} "
            f"-v {NEBULA_BIN}:/usr/local/bin/nebula "
            f"-v {NEBULA_CONFIG}:/root/.config/nebula/config.toml "
            f"{image} sleep 700"
        )
        code, out = run(docker_run, timeout=20)
        if code != 0:
            return {"instance_id": iid, "status": "ERROR", "reason": f"start: {out[-80:]}"}

        log(f"  reset to {base_commit[:12]} + apply test_patch + pre-check...")
        if patch.strip():
            ok, _ = write_to_container(container, patch, "/tmp/swe_test.patch")
            if not ok:
                return {"instance_id": iid, "status": "ERROR", "reason": "failed to copy test_patch"}

        setup_and_check = f"""
set -o pipefail
{ACTIVATE}
cd /testbed
git reset --hard {base_commit} && git clean -fd && rm -rf /testbed/.nebula
{f'git apply --whitespace=fix /tmp/swe_test.patch' if patch.strip() else 'echo no_patch'}
python -m pytest {t_args} -x --tb=short -q 2>&1 | tail -25
"""
        pcode, pre = dexec(container, setup_and_check, timeout=180)

        no_tests_ran = "no tests ran" in pre.lower()
        if pcode == 0 and not no_tests_ran:
            log("  ⚠️  tests already passing — skip")
            return {"instance_id": iid, "status": "SKIP", "reason": "already passing after reset"}
        if no_tests_ran:
            log(f"  ⚠️  test not found: {pre[-200:]}")
            return {"instance_id": iid, "status": "ERROR", "reason": "test not found after test_patch"}
        log(f"  pre-check: failing ✓ (exit {pcode})")

        # Build goal
        goal = (
            f"Fix the Python source code in /testbed so the following tests pass: {t_args}. "
            f"The tests already exist and are currently failing. "
            f"Do NOT modify any test files. "
            f"Problem: {prob[:600]}"
        )
        write_to_container(container, goal, "/tmp/nebula_goal.txt")
        # Write failing tests (newline-separated) and test patch to container
        failing_tests_str = "\n".join(fail_tests)
        write_to_container(container, failing_tests_str, "/tmp/nebula_failing_tests.txt")
        write_to_container(container, patch, "/tmp/nebula_test_patch.txt")

        nebula_cmd = (
            f'cd /testbed && '
            f'nebula --dangerously-skip-permissions do '
            f'--failing-tests "$(cat /tmp/nebula_failing_tests.txt)" '
            f'--test-patch "$(cat /tmp/nebula_test_patch.txt)" '
            f'--instance-timeout {INSTANCE_TIMEOUT}s '
            f'"$(cat /tmp/nebula_goal.txt)" > /tmp/nebula_out.log 2>&1; '
            f'rc=$?; cat /tmp/nebula_out.log; exit $rc'
        )
        log(f"  running nebula multi-agent harness (timeout={INSTANCE_TIMEOUT}s)...")
        t0 = time.time()
        ncode, nout = dexec(container, nebula_cmd, timeout=INSTANCE_TIMEOUT + 120)
        elapsed = time.time() - t0
        log(f"  nebula done in {elapsed:.0f}s (exit {ncode})")

        for line in nout.splitlines():
            if any(kw in line for kw in ["PHASE","Planner","Executor","Verifier","Critic","patch","PASS","FAIL","Error","error","StrReplace","write_file","DoGoal summary","step "]):
                log(f"    {line[:200]}")

        # Verify
        log("  verifying fix...")
        vcode, vout = dexec(container, test_cmd, timeout=120)
        # A run only counts if nebula itself reported resolved (exit 0) AND the
        # independent verify passes; guards against false PASS on zero-edit runs.
        passed = vcode == 0 and ncode == 0
        log(f"  {'PASS ✅' if passed else 'FAIL ❌'}")
        if not passed:
            for line in vout.strip().splitlines()[-10:]:
                log(f"    {line}")

        return {
            "instance_id": iid,
            "status":     "PASS" if passed else "FAIL",
            "elapsed_s":  round(elapsed, 1),
            "nebula_out": nout[-2000:],
            "verify_out": vout[-500:],
        }

    except subprocess.TimeoutExpired:
        _, tail = run(f"docker exec {container} tail -c 6000 /tmp/nebula_out.log", timeout=15)
        log("  TIMEOUT — last nebula output:")
        for line in tail.strip().splitlines()[-40:]:
            log(f"    {line[:200]}")
        return {"instance_id": iid, "status": "FAIL", "reason": "timeout"}
    except Exception as e:
        return {"instance_id": iid, "status": "ERROR", "reason": str(e)}
    finally:
        run(f"docker rm -f {container}", timeout=15)


def main():
    open(LOG_FILE, "w").close()

    if not os.path.exists(DATA_FILE):
        log(f"ERROR: {DATA_FILE} not found — run the data fetch script first")
        sys.exit(1)

    with open(DATA_FILE) as f:
        data = json.load(f)

    keys = get_keys()
    log(f"Keys loaded: {[k for k,v in keys.items() if v]}")
    if not any(keys.values()):
        log("ERROR: no API keys found"); sys.exit(1)

    log(f"SWE-bench 5-instance run — Nebula multi-agent harness (all 16 fixes applied)")
    log(f"Instances: {list(IMAGES.keys())}")

    results = []
    for iid in IMAGES:
        res = run_instance(iid, data[iid], keys)
        results.append(res)
        with open(RESULTS_FILE, "w") as f:
            json.dump(results, f, indent=2)
        log(f"  → {res['status']} {res.get('reason','')}")

    attempted = [r for r in results if r["status"] in ("PASS","FAIL")]
    passed    = [r for r in results if r["status"] == "PASS"]

    log(f"\n{'='*60}")
    log(f"FINAL SCORE: {len(passed)}/{len(attempted)} = {100*len(passed)/max(len(attempted),1):.0f}%")
    for r in results:
        mark = "✅" if r["status"]=="PASS" else ("⏭" if r["status"]=="SKIP" else "❌")
        t    = f"  {r.get('elapsed_s','-')}s" if "elapsed_s" in r else ""
        log(f"  {mark} {r['instance_id']}{t}  {r.get('reason','')}")
    log(f"\nResults: {RESULTS_FILE}  |  Log: {LOG_FILE}")

if __name__ == "__main__":
    main()
