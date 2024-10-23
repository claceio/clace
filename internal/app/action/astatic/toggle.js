// Check for saved theme on page load and update the toggle
document.addEventListener('DOMContentLoaded', (event) => {
    const theme = localStorage.getItem('theme');
    const systemDark = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;

    toggle = document.getElementById('theme-toggle') 
    if (theme == 'theme-dark' || (theme == null && systemDark)) {
        toggle.checked = true;
    } else {
        toggle.checked = false;
    }

    // Update theme switch logic to save preference
    toggle.addEventListener('change', function(event){
        if(event.target.checked){
            document.documentElement.setAttribute("data-theme", localStorage.getItem('theme-dark'))
            localStorage.setItem('theme', 'dark');
        } else {
            document.documentElement.setAttribute("data-theme", localStorage.getItem('theme-light'))
            localStorage.setItem('theme', 'light');
        }
    });

});