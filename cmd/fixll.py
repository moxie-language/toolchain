#!/usr/bin/env python3
import re, sys, subprocess

path = sys.argv[1]

for iteration in range(100):
    result = subprocess.run(
        ['clang-21', '--target=x86_64-unknown-linux-musleabihf', '-c', '-o', '/dev/null', path],
        capture_output=True, text=True
    )
    if result.returncode == 0:
        print(f"CLEAN after {iteration} iterations")
        sys.exit(0)

    err = result.stderr.split('\n')[0]
    if 'error:' not in err:
        print(f"Non-error output: {err}")
        sys.exit(1)

    print(f"iter {iteration}: {err}")

    lines = open(path).readlines()

    # Parse error line number
    m = re.search(r':(\d+):\d+: error:', err)
    if not m:
        print("  can't parse line number")
        sys.exit(1)
    err_line = int(m.group(1)) - 1

    # Pattern 1: '%reg' defined with type 'ptr' but expected '{...}'
    m = re.search(r"'(%\S+)' defined with type 'ptr' but expected '(\{[^']+\})'", err)
    if m:
        reg, expected = m.group(1), m.group(2)
        for j in range(err_line - 1, max(err_line - 200, 0), -1):
            if lines[j].strip().startswith(reg + ' = load ptr,'):
                lines[j] = lines[j].replace(reg + ' = load ptr,', reg + ' = load ' + expected + ',')
                open(path, 'w').writelines(lines)
                print(f"  fixed load at line {j+1}")
                break
            if lines[j].strip().startswith(reg + ' = '):
                print(f"  {reg} defined at line {j+1} as: {lines[j].rstrip()}")
                # If it's a function param, fix the define line
                for k in range(j, max(j-500, 0), -1):
                    if lines[k].startswith('define'):
                        if 'ptr ' + reg in lines[k]:
                            lines[k] = lines[k].replace('ptr ' + reg, expected + ' ' + reg, 1)
                            open(path, 'w').writelines(lines)
                            print(f"  fixed param in define at line {k+1}")
                        break
                break
        continue

    # Pattern: store iN %reg where %reg is ptr param
    m = re.search(r"'(%\S+)' defined with type 'ptr' but expected '(i\d+)'", err)
    if m:
        reg, expected = m.group(1), m.group(2)
        for j in range(err_line - 1, max(err_line - 500, 0), -1):
            if lines[j].startswith('define') and 'ptr ' + reg in lines[j]:
                lines[j] = lines[j].replace('ptr ' + reg, expected + ' ' + reg, 1)
                open(path, 'w').writelines(lines)
                print(f'  fixed param type at line {j+1}')
                break
        continue

    # Pattern: '%reg' defined with type 'iN' but expected 'ptr'
    m = re.search(r"'(%\S+)' defined with type '(i\d+)' but expected 'ptr'", err)
    if m:
        reg, actual = m.group(1), m.group(2)
        fix_reg = reg + '.itp' + str(iteration)
        fix_line = '  ' + fix_reg + ' = inttoptr ' + actual + ' ' + reg + ' to ptr\n'
        lines[err_line] = lines[err_line].replace('ptr ' + reg, 'ptr ' + fix_reg)
        lines.insert(err_line, fix_line)
        open(path, 'w').writelines(lines)
        print(f"  inserted inttoptr at line {err_line+1}")
        continue

    # Pattern: '%reg' defined with type '{...}' but expected 'iN'
    m = re.search(r"'(%\S+)' defined with type '(\{[^']+\})' but expected '(i\d+)'", err)
    if m:
        reg, actual, expected = m.group(1), m.group(2), m.group(3)
        for j in range(err_line - 1, max(err_line - 200, 0), -1):
            if lines[j].strip().startswith(reg + ' = load ' + actual + ','):
                lines[j] = lines[j].replace('load ' + actual + ',', 'load ' + expected + ',')
                open(path, 'w').writelines(lines)
                print(f"  fixed struct->int load at line {j+1}")
                break
            if lines[j].strip().startswith(reg + ' = '):
                break
        continue

    # Pattern 2: '%reg' defined with type '{...}' but expected 'ptr'
    m = re.search(r"'(%\S+)' defined with type '(\{[^']+\})' but expected 'ptr'", err)
    if m:
        reg, actual = m.group(1), m.group(2)
        fix_reg = reg + '.fix' + str(iteration)
        fix_line = '  ' + fix_reg + ' = extractvalue ' + actual + ' ' + reg + ', 0\n'
        lines[err_line] = lines[err_line].replace('ptr ' + reg, 'ptr ' + fix_reg)
        lines.insert(err_line, fix_line)
        open(path, 'w').writelines(lines)
        print(f"  inserted extractvalue at line {err_line+1}")
        continue

    # Pattern 3: trunc to void
    if 'void type only allowed' in err:
        m = re.search(r'trunc (i\d+) (%\S+) to void', lines[err_line])
        if m:
            lines[err_line] = lines[err_line].replace(
                'trunc ' + m.group(1) + ' ' + m.group(2) + ' to void',
                'add ' + m.group(1) + ' ' + m.group(2) + ', 0'
            )
            open(path, 'w').writelines(lines)
            print(f"  fixed trunc to void at line {err_line+1}")
            continue

    # Pattern 4: null must be a pointer type
    if 'null must be a pointer' in err:
        lines[err_line] = lines[err_line].replace(' null,', ' zeroinitializer,')
        open(path, 'w').writelines(lines)
        print(f"  fixed null -> zeroinitializer at line {err_line+1}")
        continue

    # Pattern: integer constant must have integer type (ptr used where int expected)
    if 'integer constant must have integer type' in err:
        line = lines[err_line]
        # Fix: icmp eq ptr %x, N -> icmp eq i32 %x, N (with ptrtoint)
        m2 = re.search(r'icmp (\S+) ptr (%\S+), (\d+)', line)
        if m2:
            fix_reg = m2.group(2) + '.pti' + str(iteration)
            fix_line = '  ' + fix_reg + ' = ptrtoint ptr ' + m2.group(2) + ' to i32\n'
            lines[err_line] = line.replace(
                'icmp ' + m2.group(1) + ' ptr ' + m2.group(2) + ', ' + m2.group(3),
                'icmp ' + m2.group(1) + ' i32 ' + fix_reg + ', ' + m2.group(3)
            )
            lines.insert(err_line, fix_line)
            open(path, 'w').writelines(lines)
            print(f"  fixed ptr/int comparison at line {err_line+1}")
            continue

    # Pattern: expected type 'ptr' (generic)
    if "expected 'ptr'" in err:
        m2 = re.search(r"'(%\S+)' defined with type '(\S+)' but expected 'ptr'", err)
        if m2:
            reg, actual = m2.group(1), m2.group(2)
            if actual.startswith('i'):
                fix_reg = reg + '.itp' + str(iteration)
                fix_line = '  ' + fix_reg + ' = inttoptr ' + actual + ' ' + reg + ' to ptr\n'
                lines[err_line] = lines[err_line].replace('ptr ' + reg, 'ptr ' + fix_reg)
                lines.insert(err_line, fix_line)
                open(path, 'w').writelines(lines)
                print(f"  inserted inttoptr for {actual} at line {err_line+1}")
                continue

    # Pattern: iN where struct expected (corrupted type)
    m = re.search(r"'(%\S+)' defined with type '(i\d+)' but expected '(\{[^']+\})'", err)
    if m:
        reg, actual, expected = m.group(1), m.group(2), m.group(3)
        for j in range(err_line - 1, max(err_line - 200, 0), -1):
            if lines[j].strip().startswith(reg + ' = load ' + actual + ','):
                lines[j] = lines[j].replace('load ' + actual + ',', 'load ' + expected + ',')
                open(path, 'w').writelines(lines)
                print(f'  fixed load type at line {j+1}')
                break
            if lines[j].strip().startswith(reg + ' = '):
                print(f'  {reg} defined differently: {lines[j].rstrip()}')
                break
        continue

    print(f"  UNHANDLED: {err}")
    sys.exit(1)

print("Hit max iterations")
sys.exit(1)
